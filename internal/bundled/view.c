/* Access 保存查询（View）读取和 SQL 还原。
 *
 * 数据来自 MSysObjects 与 MSysQueries。这里保留 mdb-queries 的基础行为，
 * 并补齐普通 SELECT 查询的 JOIN、参数、别名、GROUP BY、HAVING、多字段排序、
 * 外部数据库和 OWNERACCESS 等结构。
 */

typedef struct mdbgo_view_buf {
    char *data;
    size_t len;
    size_t cap;
} mdbgo_view_buf_t;

typedef struct mdbgo_view_row {
    int attribute;
    int flag;
    char *name1;
    char *name2;
    char *expression;
} mdbgo_view_row_t;

typedef struct mdbgo_view_rows {
    mdbgo_view_row_t *items;
    size_t count;
    size_t cap;
} mdbgo_view_rows_t;

typedef struct mdbgo_view_source {
    char *sql;
    char **keys;
    size_t key_count;
} mdbgo_view_source_t;

static int mdbgo_view_buf_reserve(mdbgo_view_buf_t *buf, size_t extra) {
    size_t required;
    size_t new_cap;
    char *new_data;

    if (!buf || extra > (size_t)-1 - buf->len - 1) {
        return -1;
    }
    required = buf->len + extra + 1;
    if (required <= buf->cap) {
        return 0;
    }
    new_cap = buf->cap ? buf->cap : 128;
    while (new_cap < required) {
        if (new_cap > (size_t)-1 / 2) {
            new_cap = required;
            break;
        }
        new_cap *= 2;
    }
    new_data = (char *)realloc(buf->data, new_cap);
    if (!new_data) {
        return -1;
    }
    buf->data = new_data;
    buf->cap = new_cap;
    if (buf->len == 0) {
        buf->data[0] = '\0';
    }
    return 0;
}

static int mdbgo_view_buf_append_n(mdbgo_view_buf_t *buf, const char *value, size_t value_len) {
    if (!value) {
        return 0;
    }
    if (mdbgo_view_buf_reserve(buf, value_len) != 0) {
        return -1;
    }
    memcpy(buf->data + buf->len, value, value_len);
    buf->len += value_len;
    buf->data[buf->len] = '\0';
    return 0;
}

static int mdbgo_view_buf_append(mdbgo_view_buf_t *buf, const char *value) {
    return mdbgo_view_buf_append_n(buf, value, value ? strlen(value) : 0);
}

static int mdbgo_view_buf_appendf(mdbgo_view_buf_t *buf, const char *fmt, ...) {
    va_list ap;
    va_list copy;
    int needed;

    va_start(ap, fmt);
    va_copy(copy, ap);
    needed = vsnprintf(NULL, 0, fmt, copy);
    va_end(copy);
    if (needed < 0 || mdbgo_view_buf_reserve(buf, (size_t)needed) != 0) {
        va_end(ap);
        return -1;
    }
    (void)vsnprintf(buf->data + buf->len, buf->cap - buf->len, fmt, ap);
    va_end(ap);
    buf->len += (size_t)needed;
    return 0;
}

static void mdbgo_view_buf_free(mdbgo_view_buf_t *buf) {
    if (!buf) {
        return;
    }
    free(buf->data);
    memset(buf, 0, sizeof(*buf));
}

static char *mdbgo_view_buf_take(mdbgo_view_buf_t *buf) {
    char *result;
    if (!buf) {
        return NULL;
    }
    if (!buf->data) {
        buf->data = strdup("");
    }
    result = buf->data;
    memset(buf, 0, sizeof(*buf));
    return result;
}

static int mdbgo_view_append_identifier(mdbgo_view_buf_t *buf, const char *name) {
    const char *p;
    if (!name || !name[0]) {
        return 0;
    }
    if (name[0] == '[' && name[strlen(name) - 1] == ']') {
        return mdbgo_view_buf_append(buf, name);
    }
    if (mdbgo_view_buf_append(buf, "[") != 0) {
        return -1;
    }
    for (p = name; *p; p++) {
        if (*p == ']' && mdbgo_view_buf_append(buf, "]") != 0) {
            return -1;
        }
        if (mdbgo_view_buf_append_n(buf, p, 1) != 0) {
            return -1;
        }
    }
    return mdbgo_view_buf_append(buf, "]");
}

static void mdbgo_view_rows_free(mdbgo_view_rows_t *rows) {
    size_t i;
    if (!rows) {
        return;
    }
    for (i = 0; i < rows->count; i++) {
        free(rows->items[i].name1);
        free(rows->items[i].name2);
        free(rows->items[i].expression);
    }
    free(rows->items);
    memset(rows, 0, sizeof(*rows));
}

static int mdbgo_view_rows_add(mdbgo_view_rows_t *rows, int attribute, int flag,
                               const char *name1, const char *name2, const char *expression) {
    mdbgo_view_row_t *item;
    mdbgo_view_row_t *new_items;
    size_t new_cap;

    if (rows->count == rows->cap) {
        new_cap = rows->cap ? rows->cap * 2 : 32;
        new_items = (mdbgo_view_row_t *)realloc(rows->items, new_cap * sizeof(*new_items));
        if (!new_items) {
            return -1;
        }
        rows->items = new_items;
        rows->cap = new_cap;
    }
    item = &rows->items[rows->count];
    memset(item, 0, sizeof(*item));
    item->attribute = attribute;
    item->flag = flag;
    item->name1 = (name1 && name1[0]) ? strdup(name1) : NULL;
    item->name2 = (name2 && name2[0]) ? strdup(name2) : NULL;
    item->expression = (expression && expression[0]) ? strdup(expression) : NULL;
    if (((name1 && name1[0]) && !item->name1) ||
        ((name2 && name2[0]) && !item->name2) ||
        ((expression && expression[0]) && !item->expression)) {
        free(item->name1);
        free(item->name2);
        free(item->expression);
        memset(item, 0, sizeof(*item));
        return -1;
    }
    rows->count++;
    return 0;
}

static MdbCatalogEntry *mdbgo_view_catalog_entry(MdbHandle *mdb, const char *name, int object_type) {
    guint i;
    if (!mdb || !mdb->catalog || !name) {
        return NULL;
    }
    for (i = 0; i < mdb->catalog->len; i++) {
        MdbCatalogEntry *entry = g_ptr_array_index(mdb->catalog, i);
        if (entry && (object_type == MDB_ANY || entry->object_type == object_type) &&
            g_ascii_strcasecmp(entry->object_name, name) == 0) {
            return entry;
        }
    }
    return NULL;
}

int mdbgo_list_views(mdbgo_db_t *db, char ***out_names, size_t *out_count, char *err, size_t err_len) {
    GPtrArray *catalog;
    char **names;
    size_t count = 0;
    guint i;

    if (!db || !db->mdb || !out_names || !out_count) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    *out_names = NULL;
    *out_count = 0;
    catalog = mdb_read_catalog(db->mdb, MDB_ANY);
    if (!catalog) {
        mdbgo_set_error(err, err_len, "failed to read catalog");
        return -1;
    }
    names = (char **)calloc((size_t)catalog->len, sizeof(char *));
    if (!names) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }
    for (i = 0; i < catalog->len; i++) {
        MdbCatalogEntry *entry = g_ptr_array_index(catalog, i);
        if (!entry || entry->object_type != MDB_QUERY) {
            continue;
        }
        names[count] = strdup(entry->object_name);
        if (!names[count]) {
            mdbgo_free_partial_string_array(names, count);
            mdbgo_set_error(err, err_len, "out of memory");
            return -1;
        }
        count++;
    }
    *out_names = names;
    *out_count = count;
    return 0;
}

void mdbgo_free_string(char *value) {
    free(value);
}

static char *mdbgo_view_find_object_id(MdbCatalogEntry *msys_objects, const char *view_name) {
    MdbTableDef *table = NULL;
    char *id = NULL;
    char *name = NULL;
    char *result = NULL;
    size_t bind_size;

    if (!msys_objects || !view_name) {
        return NULL;
    }
    bind_size = msys_objects->mdb->bind_size;
    id = (char *)calloc(1, bind_size);
    name = (char *)calloc(1, bind_size);
    if (!id || !name) {
        goto cleanup;
    }
    table = mdb_read_table(msys_objects);
    if (!table || !mdb_read_columns(table) ||
        mdb_bind_column_by_name(table, "Id", id, NULL) == -1 ||
        mdb_bind_column_by_name(table, "Name", name, NULL) == -1) {
        goto cleanup;
    }
    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        if (g_ascii_strcasecmp(view_name, name) == 0) {
            result = strdup(id);
            break;
        }
    }

cleanup:
    if (table) {
        mdb_free_tabledef(table);
    }
    free(id);
    free(name);
    return result;
}

static int mdbgo_view_load_rows(MdbCatalogEntry *msys_queries, const char *object_id,
                                mdbgo_view_rows_t *rows) {
    MdbTableDef *table = NULL;
    char *attribute = NULL;
    char *expression = NULL;
    char *flag = NULL;
    char *name1 = NULL;
    char *name2 = NULL;
    char *row_object_id = NULL;
    size_t bind_size;
    int rc = -1;

    bind_size = msys_queries->mdb->bind_size;
    attribute = (char *)calloc(1, bind_size);
    expression = (char *)calloc(1, bind_size);
    flag = (char *)calloc(1, bind_size);
    name1 = (char *)calloc(1, bind_size);
    name2 = (char *)calloc(1, bind_size);
    row_object_id = (char *)calloc(1, bind_size);
    if (!attribute || !expression || !flag || !name1 || !name2 || !row_object_id) {
        goto cleanup;
    }
    table = mdb_read_table(msys_queries);
    if (!table || !mdb_read_columns(table) ||
        mdb_bind_column_by_name(table, "Attribute", attribute, NULL) == -1 ||
        mdb_bind_column_by_name(table, "Expression", expression, NULL) == -1 ||
        mdb_bind_column_by_name(table, "Flag", flag, NULL) == -1 ||
        mdb_bind_column_by_name(table, "Name1", name1, NULL) == -1 ||
        mdb_bind_column_by_name(table, "Name2", name2, NULL) == -1 ||
        mdb_bind_column_by_name(table, "ObjectId", row_object_id, NULL) == -1) {
        goto cleanup;
    }
    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        if (strcmp(object_id, row_object_id) != 0) {
            continue;
        }
        if (mdbgo_view_rows_add(rows, atoi(attribute), atoi(flag), name1, name2, expression) != 0) {
            goto cleanup;
        }
    }
    rc = rows->count ? 0 : -1;

cleanup:
    if (table) {
        mdb_free_tabledef(table);
    }
    free(attribute);
    free(expression);
    free(flag);
    free(name1);
    free(name2);
    free(row_object_id);
    return rc;
}

static const char *mdbgo_view_param_type(int flag) {
    switch (flag) {
        case 0: return "Value";
        case MDB_BOOL: return "Bit";
        case MDB_TEXT: return "Text";
        case MDB_BYTE: return "Byte";
        case MDB_INT: return "Short";
        case MDB_LONGINT: return "Long";
        case MDB_MONEY: return "Currency";
        case MDB_FLOAT: return "IEEESingle";
        case MDB_DOUBLE: return "IEEEDouble";
        case MDB_DATETIME: return "DateTime";
        case MDB_BINARY: return "Binary";
        case MDB_OLE: return "LongBinary";
        case MDB_REPID: return "Guid";
        default: return NULL;
    }
}

static void mdbgo_view_source_free(mdbgo_view_source_t *source) {
    size_t i;
    if (!source) {
        return;
    }
    free(source->sql);
    for (i = 0; i < source->key_count; i++) {
        free(source->keys[i]);
    }
    free(source->keys);
    memset(source, 0, sizeof(*source));
}

static int mdbgo_view_source_contains(const mdbgo_view_source_t *source, const char *key) {
    size_t i;
    for (i = 0; source && key && i < source->key_count; i++) {
        if (g_ascii_strcasecmp(source->keys[i], key) == 0) {
            return 1;
        }
    }
    return 0;
}

static int mdbgo_view_build_table_source(const mdbgo_view_row_t *row, mdbgo_view_source_t *source) {
    mdbgo_view_buf_t buf = {0};
    const char *key;
    if (row->expression) {
        if (mdbgo_view_append_identifier(&buf, row->expression) != 0 ||
            mdbgo_view_buf_append(&buf, ".") != 0) {
            goto fail;
        }
    }
    if (mdbgo_view_append_identifier(&buf, row->name1) != 0) {
        goto fail;
    }
    if (row->name2) {
        if (mdbgo_view_buf_append(&buf, " AS ") != 0 ||
            mdbgo_view_append_identifier(&buf, row->name2) != 0) {
            goto fail;
        }
    }
    key = row->name2 ? row->name2 : row->name1;
    source->keys = (char **)calloc(1, sizeof(char *));
    if (!source->keys) {
        goto fail;
    }
    source->keys[0] = key ? strdup(key) : NULL;
    if (!source->keys[0]) {
        goto fail;
    }
    source->key_count = 1;
    source->sql = mdbgo_view_buf_take(&buf);
    return source->sql ? 0 : -1;

fail:
    mdbgo_view_buf_free(&buf);
    mdbgo_view_source_free(source);
    return -1;
}

static int mdbgo_view_same_join_pair(const mdbgo_view_row_t *a, const mdbgo_view_row_t *b) {
    if (!a->name1 || !a->name2 || !b->name1 || !b->name2 || a->flag != b->flag) {
        return 0;
    }
    return g_ascii_strcasecmp(a->name1, b->name1) == 0 &&
           g_ascii_strcasecmp(a->name2, b->name2) == 0;
}

static int mdbgo_view_build_from(const mdbgo_view_rows_t *rows, char **out_from,
                                 char *err, size_t err_len) {
    mdbgo_view_source_t *sources = NULL;
    unsigned char *join_used = NULL;
    size_t source_count = 0;
    size_t table_count = 0;
    size_t i;
    size_t j;
    int rc = -1;
    mdbgo_view_buf_t result = {0};

    for (i = 0; i < rows->count; i++) {
        if (rows->items[i].attribute == 5) table_count++;
    }
    if (table_count == 0) {
        *out_from = strdup("");
        return *out_from ? 0 : -1;
    }
    sources = (mdbgo_view_source_t *)calloc(table_count, sizeof(*sources));
    join_used = (unsigned char *)calloc(rows->count, 1);
    if (!sources || !join_used) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto cleanup;
    }
    for (i = 0; i < rows->count; i++) {
        if (rows->items[i].attribute != 5) {
            continue;
        }
        if (mdbgo_view_build_table_source(&rows->items[i], &sources[source_count]) != 0) {
            mdbgo_set_error(err, err_len, "failed to build view table source");
            goto cleanup;
        }
        source_count++;
    }

    for (i = 0; i < rows->count; i++) {
        const mdbgo_view_row_t *join = &rows->items[i];
        mdbgo_view_buf_t condition = {0};
        mdbgo_view_buf_t combined = {0};
        size_t from_index = (size_t)-1;
        size_t to_index = (size_t)-1;
        const char *join_type;
        char **new_keys;
        size_t old_key_count;

        if (join->attribute != 7 || join_used[i]) {
            continue;
        }
        join_used[i] = 1;
        if (!join->name1 || !join->name2 || !join->expression) {
            mdbgo_set_error(err, err_len, "invalid join row in view definition");
            mdbgo_view_buf_free(&condition);
            goto cleanup;
        }
        if (mdbgo_view_buf_append(&condition, "(") != 0 ||
            mdbgo_view_buf_append(&condition, join->expression) != 0 ||
            mdbgo_view_buf_append(&condition, ")") != 0) {
            mdbgo_view_buf_free(&condition);
            goto oom;
        }
        for (j = i + 1; j < rows->count; j++) {
            if (!join_used[j] && rows->items[j].attribute == 7 &&
                mdbgo_view_same_join_pair(join, &rows->items[j])) {
                join_used[j] = 1;
                if (mdbgo_view_buf_append(&condition, " AND (") != 0 ||
                    mdbgo_view_buf_append(&condition, rows->items[j].expression) != 0 ||
                    mdbgo_view_buf_append(&condition, ")") != 0) {
                    mdbgo_view_buf_free(&condition);
                    goto oom;
                }
            }
        }
        for (j = 0; j < source_count; j++) {
            if (from_index == (size_t)-1 && mdbgo_view_source_contains(&sources[j], join->name1)) {
                from_index = j;
            }
            if (to_index == (size_t)-1 && mdbgo_view_source_contains(&sources[j], join->name2)) {
                to_index = j;
            }
        }
        if (from_index == (size_t)-1 || to_index == (size_t)-1) {
            mdbgo_set_error(err, err_len, "view join references an unknown table: %s -> %s", join->name1, join->name2);
            mdbgo_view_buf_free(&condition);
            goto cleanup;
        }
        if (from_index == to_index) {
            mdbgo_set_error(err, err_len, "cannot safely reconstruct cyclic join: %s -> %s", join->name1, join->name2);
            mdbgo_view_buf_free(&condition);
            goto cleanup;
        }
        if (join->flag == 1) join_type = "INNER JOIN";
        else if (join->flag == 2) join_type = "LEFT JOIN";
        else if (join->flag == 3) join_type = "RIGHT JOIN";
        else {
            mdbgo_set_error(err, err_len, "unknown Access join type: %d", join->flag);
            mdbgo_view_buf_free(&condition);
            goto cleanup;
        }
        if (mdbgo_view_buf_appendf(&combined, "(%s %s %s ON %s)",
                                   sources[from_index].sql, join_type,
                                   sources[to_index].sql, condition.data) != 0) {
            mdbgo_view_buf_free(&condition);
            mdbgo_view_buf_free(&combined);
            goto oom;
        }
        mdbgo_view_buf_free(&condition);
        free(sources[from_index].sql);
        sources[from_index].sql = mdbgo_view_buf_take(&combined);
        old_key_count = sources[from_index].key_count;
        new_keys = (char **)realloc(sources[from_index].keys,
                                    (old_key_count + sources[to_index].key_count) * sizeof(char *));
        if (!new_keys) {
            goto oom;
        }
        sources[from_index].keys = new_keys;
        memcpy(sources[from_index].keys + old_key_count, sources[to_index].keys,
               sources[to_index].key_count * sizeof(char *));
        sources[from_index].key_count += sources[to_index].key_count;
        free(sources[to_index].keys);
        free(sources[to_index].sql);
        memset(&sources[to_index], 0, sizeof(sources[to_index]));
        if (to_index != source_count - 1) {
            sources[to_index] = sources[source_count - 1];
            memset(&sources[source_count - 1], 0, sizeof(sources[source_count - 1]));
        }
        source_count--;
    }

    for (i = 0; i < source_count; i++) {
        if (i && mdbgo_view_buf_append(&result, ", ") != 0) {
            goto oom;
        }
        if (mdbgo_view_buf_append(&result, sources[i].sql) != 0) {
            goto oom;
        }
    }
    *out_from = mdbgo_view_buf_take(&result);
    rc = *out_from ? 0 : -1;
    goto cleanup;

oom:
    mdbgo_set_error(err, err_len, "out of memory");
cleanup:
    mdbgo_view_buf_free(&result);
    if (sources) {
        for (i = 0; i < table_count; i++) {
            mdbgo_view_source_free(&sources[i]);
        }
    }
    free(sources);
    free(join_used);
    return rc;
}

static int mdbgo_view_append_list(mdbgo_view_buf_t *sql, const mdbgo_view_rows_t *rows,
                                  int attribute, int aliases, int descending) {
    size_t i;
    int appended = 0;
    for (i = 0; i < rows->count; i++) {
        const mdbgo_view_row_t *row = &rows->items[i];
        if (row->attribute != attribute || !row->expression) {
            continue;
        }
        if (appended && mdbgo_view_buf_append(sql, ", ") != 0) return -1;
        if (mdbgo_view_buf_append(sql, row->expression) != 0) return -1;
        if (aliases && row->name1) {
            if (mdbgo_view_buf_append(sql, " AS ") != 0 ||
                mdbgo_view_append_identifier(sql, row->name1) != 0) return -1;
        }
        if (descending && row->name1 && g_ascii_strcasecmp(row->name1, "D") == 0 &&
            mdbgo_view_buf_append(sql, " DESC") != 0) return -1;
        appended = 1;
    }
    return appended;
}

static const mdbgo_view_row_t *mdbgo_view_first_row(const mdbgo_view_rows_t *rows, int attribute) {
    size_t i;
    for (i = 0; i < rows->count; i++) {
        if (rows->items[i].attribute == attribute) {
            return &rows->items[i];
        }
    }
    return NULL;
}

static int mdbgo_view_build_select_sql(const mdbgo_view_rows_t *rows, int object_flags, char **out_sql,
                                       char *err, size_t err_len) {
    mdbgo_view_buf_t sql = {0};
    const mdbgo_view_row_t *flag_row = mdbgo_view_first_row(rows, 3);
    const mdbgo_view_row_t *remote_row = mdbgo_view_first_row(rows, 4);
    const mdbgo_view_row_t *where_row = mdbgo_view_first_row(rows, 8);
    const mdbgo_view_row_t *having_row = mdbgo_view_first_row(rows, 10);
    char *from = NULL;
    int select_flags = flag_row ? flag_row->flag : 0;
    size_t i;
    int list_rc;

    if ((object_flags & 0xF0) != 0) {
        mdbgo_set_error(err, err_len, "unsupported Access query object flags: 0x%x (only SELECT views are supported)", object_flags);
        return -1;
    }
    for (i = 0; i < rows->count; i++) {
        const mdbgo_view_row_t *row = &rows->items[i];
        const char *param_type;
        if (row->attribute != 2 || !row->name1) continue;
        if (sql.len == 0 || (sql.len >= 2 && strcmp(sql.data + sql.len - 2, ";\n") == 0)) {
            if (sql.len == 0 && mdbgo_view_buf_append(&sql, "PARAMETERS ") != 0) goto oom;
        }
        param_type = mdbgo_view_param_type(row->flag);
        if (!param_type) {
            mdbgo_set_error(err, err_len, "unknown Access parameter type: %d", row->flag);
            goto fail;
        }
        if (i > 0) {
            size_t k;
            int previous_param = 0;
            for (k = 0; k < i; k++) if (rows->items[k].attribute == 2) previous_param = 1;
            if (previous_param && mdbgo_view_buf_append(&sql, ", ") != 0) goto oom;
        }
        if (mdbgo_view_append_identifier(&sql, row->name1) != 0 ||
            mdbgo_view_buf_appendf(&sql, " %s", param_type) != 0) goto oom;
    }
    if (sql.len && mdbgo_view_buf_append(&sql, ";\n") != 0) goto oom;

    if (mdbgo_view_buf_append(&sql, "SELECT ") != 0) goto oom;
    if ((select_flags & 0x02) && mdbgo_view_buf_append(&sql, "DISTINCT ") != 0) goto oom;
    else if ((select_flags & 0x08) && mdbgo_view_buf_append(&sql, "DISTINCTROW ") != 0) goto oom;
    else if (select_flags & 0x10) {
        if (mdbgo_view_buf_appendf(&sql, "TOP %s%s ",
                                   (flag_row && flag_row->name1) ? flag_row->name1 : "0",
                                   (select_flags & 0x20) ? " PERCENT" : "") != 0) goto oom;
    }
    list_rc = mdbgo_view_append_list(&sql, rows, 6, 1, 0);
    if (list_rc < 0) goto oom;
    if (!list_rc && (select_flags & 0x01) && mdbgo_view_buf_append(&sql, "*") != 0) goto oom;
    if (!list_rc && !(select_flags & 0x01)) {
        mdbgo_set_error(err, err_len, "view has no SELECT columns");
        goto fail;
    }
    if (mdbgo_view_build_from(rows, &from, err, err_len) != 0) goto fail;
    if (from[0]) {
        if (mdbgo_view_buf_append(&sql, "\nFROM ") != 0 ||
            mdbgo_view_buf_append(&sql, from) != 0) goto oom;
        if (remote_row && (remote_row->name1 || remote_row->expression)) {
            if (mdbgo_view_buf_append(&sql, " IN '") != 0 ||
                mdbgo_view_buf_append(&sql, remote_row->name1 ? remote_row->name1 : "") != 0 ||
                mdbgo_view_buf_append(&sql, "'") != 0) goto oom;
            if (remote_row->expression &&
                (mdbgo_view_buf_append(&sql, " [") != 0 ||
                 mdbgo_view_buf_append(&sql, remote_row->expression) != 0 ||
                 mdbgo_view_buf_append(&sql, "]") != 0)) goto oom;
        }
    }
    if (where_row && where_row->expression) {
        if (mdbgo_view_buf_append(&sql, "\nWHERE ") != 0 ||
            mdbgo_view_buf_append(&sql, where_row->expression) != 0) goto oom;
    }
    {
        mdbgo_view_buf_t clause = {0};
        list_rc = mdbgo_view_append_list(&clause, rows, 9, 0, 0);
        if (list_rc < 0) { mdbgo_view_buf_free(&clause); goto oom; }
        if (list_rc && (mdbgo_view_buf_append(&sql, "\nGROUP BY ") != 0 ||
                        mdbgo_view_buf_append(&sql, clause.data) != 0)) {
            mdbgo_view_buf_free(&clause); goto oom;
        }
        mdbgo_view_buf_free(&clause);
    }
    if (having_row && having_row->expression &&
        (mdbgo_view_buf_append(&sql, "\nHAVING ") != 0 ||
         mdbgo_view_buf_append(&sql, having_row->expression) != 0)) goto oom;
    {
        mdbgo_view_buf_t clause = {0};
        list_rc = mdbgo_view_append_list(&clause, rows, 11, 0, 1);
        if (list_rc < 0) { mdbgo_view_buf_free(&clause); goto oom; }
        if (list_rc && (mdbgo_view_buf_append(&sql, "\nORDER BY ") != 0 ||
                        mdbgo_view_buf_append(&sql, clause.data) != 0)) {
            mdbgo_view_buf_free(&clause); goto oom;
        }
        mdbgo_view_buf_free(&clause);
    }
    if ((select_flags & 0x04) && mdbgo_view_buf_append(&sql, "\nWITH OWNERACCESS OPTION") != 0) goto oom;
    if (mdbgo_view_buf_append(&sql, ";") != 0) goto oom;
    *out_sql = mdbgo_view_buf_take(&sql);
    free(from);
    return *out_sql ? 0 : -1;

oom:
    mdbgo_set_error(err, err_len, "out of memory");
fail:
    free(from);
    mdbgo_view_buf_free(&sql);
    return -1;
}

int mdbgo_get_view_sql(mdbgo_db_t *db, const char *view_name, char **out_sql,
                       char *err, size_t err_len) {
    MdbCatalogEntry *view_entry;
    MdbCatalogEntry *msys_objects;
    MdbCatalogEntry *msys_queries;
    mdbgo_view_rows_t rows = {0};
    char *object_id = NULL;
    int rc = -1;

    if (!db || !db->mdb || !view_name || !view_name[0] || !out_sql) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    *out_sql = NULL;
    if (!mdb_read_catalog(db->mdb, MDB_ANY)) {
        mdbgo_set_error(err, err_len, "failed to read catalog");
        return -1;
    }
    view_entry = mdbgo_view_catalog_entry(db->mdb, view_name, MDB_QUERY);
    msys_objects = mdbgo_view_catalog_entry(db->mdb, "MSysObjects", MDB_ANY);
    msys_queries = mdbgo_view_catalog_entry(db->mdb, "MSysQueries", MDB_ANY);
    if (!view_entry) {
        mdbgo_set_error(err, err_len, "view not found: %s", view_name);
        goto cleanup;
    }
    if (!msys_objects || !msys_queries) {
        mdbgo_set_error(err, err_len, "MSysObjects or MSysQueries not found");
        goto cleanup;
    }
    object_id = mdbgo_view_find_object_id(msys_objects, view_entry->object_name);
    if (!object_id) {
        mdbgo_set_error(err, err_len, "failed to resolve view object id: %s", view_name);
        goto cleanup;
    }
    if (mdbgo_view_load_rows(msys_queries, object_id, &rows) != 0) {
        mdbgo_set_error(err, err_len, "failed to read MSysQueries rows for view: %s", view_name);
        goto cleanup;
    }
    rc = mdbgo_view_build_select_sql(&rows, view_entry->flags, out_sql, err, err_len);

cleanup:
    free(object_id);
    mdbgo_view_rows_free(&rows);
    return rc;
}
