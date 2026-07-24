#include "bridge.h"

#include <ctype.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "mdbtools.h"
#include "mdbsql.h"

struct mdbgo_db {
    MdbHandle *mdb;
};

/* 统一写错误信息，确保 err 缓冲区总是 NUL 结尾。 */
static void mdbgo_set_error(char *err, size_t err_len, const char *fmt, ...) {
    va_list ap;

    if (!err || err_len == 0) {
        return;
    }

    va_start(ap, fmt);
    (void)vsnprintf(err, err_len, fmt, ap);
    va_end(ap);
}

/* 释放字符串数组中前 n 个元素，供错误回滚复用。 */
static void mdbgo_free_partial_string_array(char **arr, size_t n) {
    size_t i;

    if (!arr) {
        return;
    }

    for (i = 0; i < n; i++) {
        free(arr[i]);
    }
    free(arr);
}

typedef struct mdbgo_hash_copy_ctx {
    mdbgo_property_item_t *items;
    size_t item_count;
    size_t index;
    int ok;
} mdbgo_hash_copy_ctx_t;

/* 统计属性哈希中的键值对数量，供后续一次性分配数组。 */
static void mdbgo_hash_count_cb(gpointer key, gpointer value, gpointer user_data) {
    size_t *count = (size_t *)user_data;
    (void)key;
    (void)value;
    if (!count) {
        return;
    }
    (*count)++;
}

/* 拷贝属性哈希中的键值，失败时置 ok=0 并保留已拷贝部分供统一释放。 */
static void mdbgo_hash_copy_cb(gpointer key, gpointer value, gpointer user_data) {
    mdbgo_hash_copy_ctx_t *ctx = (mdbgo_hash_copy_ctx_t *)user_data;
    char *dup_key;
    char *dup_val;

    if (!ctx || !ctx->ok || ctx->index >= ctx->item_count) {
        return;
    }

    dup_key = strdup((const char *)(key ? key : ""));
    dup_val = strdup((const char *)(value ? value : ""));
    if (!dup_key || !dup_val) {
        free(dup_key);
        free(dup_val);
        ctx->ok = 0;
        return;
    }

    ctx->items[ctx->index].key = dup_key;
    ctx->items[ctx->index].value = dup_val;
    ctx->index++;
}

/* 释放单个属性块（包含块名和全部键值项）。 */
static void mdbgo_free_property_block(mdbgo_property_block_t *block) {
    size_t i;

    if (!block) {
        return;
    }

    free(block->name);
    block->name = NULL;

    if (block->items) {
        for (i = 0; i < block->item_count; i++) {
            free(block->items[i].key);
            free(block->items[i].value);
        }
        free(block->items);
    }
    block->items = NULL;
    block->item_count = 0;
}

/* 释放单个窗体对象（包含元信息和所有属性块）。 */
static void mdbgo_free_form_info(mdbgo_form_info_t *form) {
    size_t i;

    if (!form) {
        return;
    }

    free(form->name);
    free(form->object_type_name);
    form->name = NULL;
    form->object_type_name = NULL;

    if (form->prop_blocks) {
        for (i = 0; i < form->prop_block_count; i++) {
            mdbgo_free_property_block(&form->prop_blocks[i]);
        }
        free(form->prop_blocks);
    }
    form->prop_blocks = NULL;
    form->prop_block_count = 0;
}

/* 释放窗体原始设计流结构。 */
static void mdbgo_free_form_streams_inner(mdbgo_form_streams_t *data) {
    if (!data) {
        return;
    }
    free(data->form_name);
    free(data->lv);
    free(data->lv_prop);
    free(data->lv_extra);
    memset(data, 0, sizeof(*data));
}

/* 深拷贝 OLE 数据缓冲，成功返回 0。 */
static int mdbgo_copy_stream_bytes(void *src, size_t src_len, unsigned char **out_ptr, size_t *out_len) {
    unsigned char *buf;

    if (!out_ptr || !out_len) {
        return -1;
    }

    *out_ptr = NULL;
    *out_len = 0;
    if (!src || src_len == 0) {
        return 0;
    }

    buf = (unsigned char *)malloc(src_len);
    if (!buf) {
        return -1;
    }
    memcpy(buf, src, src_len);

    *out_ptr = buf;
    *out_len = src_len;
    return 0;
}

/* 释放 blob 结构。 */
static void mdbgo_free_blob_data_inner(mdbgo_blob_data_t *out) {
    if (!out) {
        return;
    }
    free(out->data);
    out->data = NULL;
    out->len = 0;
}

static void mdbgo_free_access_object_data_array_inner(mdbgo_access_object_data_array_t *out) {
    size_t i;

    if (!out) {
        return;
    }
    for (i = 0; i < out->count; i++) {
        free(out->values[i].data);
    }
    free(out->values);
    out->values = NULL;
    out->count = 0;
}

static void mdbgo_free_access_storage_entry_array_inner(mdbgo_access_storage_entry_array_t *out) {
    size_t i;

    if (!out) {
        return;
    }
    for (i = 0; i < out->count; i++) {
        free(out->values[i].name);
        free(out->values[i].data);
    }
    free(out->values);
    out->values = NULL;
    out->count = 0;
}

static void mdbgo_free_int_array_inner(mdbgo_int_array_t *out) {
    if (!out) {
        return;
    }
    free(out->values);
    out->values = NULL;
    out->count = 0;
}

/* 将 mdb 列类型常量映射到稳定字符串，便于 Go 侧直接展示。 */
static const char *mdbgo_col_type_name(int col_type) {
    switch (col_type) {
        case MDB_BOOL:
            return "BOOL";
        case MDB_BYTE:
            return "BYTE";
        case MDB_INT:
            return "INT";
        case MDB_LONGINT:
            return "LONGINT";
        case MDB_MONEY:
            return "MONEY";
        case MDB_FLOAT:
            return "FLOAT";
        case MDB_DOUBLE:
            return "DOUBLE";
        case MDB_DATETIME:
            return "DATETIME";
        case MDB_BINARY:
            return "BINARY";
        case MDB_TEXT:
            return "TEXT";
        case MDB_OLE:
            return "OLE";
        case MDB_MEMO:
            return "MEMO";
        case MDB_REPID:
            return "REPID";
        case MDB_NUMERIC:
            return "NUMERIC";
        case MDB_COMPLEX:
            return "COMPLEX";
        default:
            return "UNKNOWN";
    }
}

/* 在失败路径中释放已经填充的 cells，避免部分构造时泄漏。 */
static void mdbgo_free_partial_cells(char **cells, size_t used_count) {
    size_t i;

    if (!cells) {
        return;
    }

    for (i = 0; i < used_count; i++) {
        free(cells[i]);
    }
    free(cells);
}

/* 忽略前导空白后，判断 query 是否以关键字开头（大小写不敏感）。 */
static int mdbgo_starts_with_keyword(const char *query, const char *keyword) {
    size_t i = 0;
    size_t j = 0;

    if (!query || !keyword) {
        return 0;
    }

    while (query[i] != '\0' && isspace((unsigned char)query[i])) {
        i++;
    }

    while (keyword[j] != '\0') {
        if (query[i + j] == '\0') {
            return 0;
        }
        if (tolower((unsigned char)query[i + j]) != tolower((unsigned char)keyword[j])) {
            return 0;
        }
        j++;
    }

    /* 关键字后必须是结尾或空白，防止误匹配到更长单词。 */
    return query[i + j] == '\0' || isspace((unsigned char)query[i + j]);
}

int mdbgo_open(const char *path, mdbgo_db_t **out_db, char *err, size_t err_len) {
    mdbgo_db_t *db;

    if (!path || !out_db) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    *out_db = NULL;
    db = (mdbgo_db_t *)calloc(1, sizeof(*db));
    if (!db) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }

    db->mdb = mdb_open(path, MDB_NOFLAGS);
    if (!db->mdb) {
        free(db);
        mdbgo_set_error(err, err_len, "failed to open mdb: %s", path);
        return -1;
    }

    /* 增大绑定缓冲区，减少长文本/二进制字段截断概率。 */
    mdb_set_bind_size(db->mdb, 256 * 1024);

    *out_db = db;
    return 0;
}

void mdbgo_close(mdbgo_db_t *db) {
    if (!db) {
        return;
    }

    if (db->mdb) {
        mdb_close(db->mdb);
        db->mdb = NULL;
    }

    free(db);
}

int mdbgo_get_file_format(
    mdbgo_db_t *db,
    int *out_version,
    size_t *out_page_size,
    char *err,
    size_t err_len
) {
    if (!db || !db->mdb || !db->mdb->f || !db->mdb->fmt || !out_version || !out_page_size) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    *out_version = (int)db->mdb->f->jet_version;
    *out_page_size = (size_t)db->mdb->fmt->pg_size;
    return 0;
}

int mdbgo_list_tables(mdbgo_db_t *db, char ***out_names, size_t *out_count, char *err, size_t err_len) {
    GPtrArray *catalog;
    char **names;
    size_t count;
    guint i;

    if (!db || !db->mdb || !out_names || !out_count) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    *out_names = NULL;
    *out_count = 0;

    catalog = mdb_read_catalog(db->mdb, MDB_TABLE);
    if (!catalog) {
        mdbgo_set_error(err, err_len, "failed to read catalog");
        return -1;
    }

    names = (char **)calloc((size_t)catalog->len, sizeof(char *));
    if (!names) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }

    count = 0;
    for (i = 0; i < catalog->len; i++) {
        MdbCatalogEntry *entry = g_ptr_array_index(catalog, i);
        char *dup_name;

        if (!entry || !mdb_is_user_table(entry)) {
            continue;
        }

        dup_name = strdup(entry->object_name);
        if (!dup_name) {
            mdbgo_free_partial_string_array(names, count);
            mdbgo_set_error(err, err_len, "out of memory");
            return -1;
        }

        names[count++] = dup_name;
    }

    *out_names = names;
    *out_count = count;
    return 0;
}

void mdbgo_free_string_array(char **arr, size_t count) {
    mdbgo_free_partial_string_array(arr, count);
}

int mdbgo_table_row_count(mdbgo_db_t *db, const char *table_name, size_t *out_count, char *err, size_t err_len) {
    MdbTableDef *table;

    if (!db || !db->mdb || !table_name || !out_count) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    *out_count = 0;
    table = mdb_read_table_by_name(db->mdb, (gchar *)table_name, MDB_TABLE);
    if (!table) {
        mdbgo_set_error(err, err_len, "table not found: %s", table_name);
        return -1;
    }

    *out_count = (size_t)table->num_rows;
    mdb_free_tabledef(table);
    return 0;
}

int mdbgo_read_table(mdbgo_db_t *db, const char *table_name, mdbgo_table_data_t *out, char *err, size_t err_len) {
    MdbTableDef *table;
    char **bound_values = NULL;
    int *bound_lens = NULL;
    char **columns = NULL;
    char **cells = NULL;
    unsigned char *nulls = NULL;
    size_t col_count;
    size_t row_count;
    size_t row_cap;
    size_t used_cell_count;
    size_t i;

    if (!db || !db->mdb || !table_name || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    memset(out, 0, sizeof(*out));

    table = mdb_read_table_by_name(db->mdb, (gchar *)table_name, MDB_TABLE);
    if (!table) {
        mdbgo_set_error(err, err_len, "table not found: %s", table_name);
        return -1;
    }

    mdb_read_columns(table);
    mdb_rewind_table(table);

    col_count = table->num_cols;
    columns = (char **)calloc(col_count, sizeof(char *));
    bound_values = (char **)calloc(col_count, sizeof(char *));
    bound_lens = (int *)calloc(col_count, sizeof(int));
    if ((!columns && col_count > 0) || (!bound_values && col_count > 0) || (!bound_lens && col_count > 0)) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }

    for (i = 0; i < col_count; i++) {
        MdbColumn *col = g_ptr_array_index(table->columns, (guint)i);
        int bind_rc;

        columns[i] = strdup(col ? col->name : "");
        bound_values[i] = (char *)calloc(1, 256 * 1024);
        if (!columns[i] || !bound_values[i]) {
            mdbgo_set_error(err, err_len, "out of memory");
            goto fail;
        }

        bind_rc = mdb_bind_column(table, (int)i + 1, bound_values[i], &bound_lens[i]);
        if (bind_rc == -1) {
            mdbgo_set_error(err, err_len, "failed to bind column %zu", i + 1);
            goto fail;
        }
    }

    row_count = 0;
    row_cap = 0;
    used_cell_count = 0;
    while (mdb_fetch_row(table)) {
        size_t base;

        if (row_count == row_cap) {
            size_t new_cap = (row_cap == 0) ? 64 : (row_cap * 2);
            char **new_cells = (char **)realloc(cells, new_cap * col_count * sizeof(char *));
            unsigned char *new_nulls;
            if (!new_cells) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            cells = new_cells;
            new_nulls = (unsigned char *)realloc(nulls, new_cap * col_count * sizeof(unsigned char));
            if (!new_nulls) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            nulls = new_nulls;
            row_cap = new_cap;
        }

        base = row_count * col_count;
        for (i = 0; i < col_count; i++) {
            MdbColumn *col = g_ptr_array_index(table->columns, (guint)i);
            int value_len = bound_lens[i];
            char *cell;

            if (value_len < 0) {
                value_len = 0;
            }

            cell = (char *)malloc((size_t)value_len + 1);
            if (!cell) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }

            if (value_len > 0) {
                memcpy(cell, bound_values[i], (size_t)value_len);
            }
            cell[value_len] = '\0';
            cells[base + i] = cell;
            nulls[base + i] = col ? col->is_null : 0;
            used_cell_count++;
        }

        row_count++;
    }

    for (i = 0; i < col_count; i++) {
        free(bound_values[i]);
    }
    free(bound_values);
    free(bound_lens);
    mdb_free_tabledef(table);

    out->columns = columns;
    out->col_count = col_count;
    out->cells = cells;
    out->nulls = nulls;
    out->row_count = row_count;
    return 0;

fail:
    mdbgo_free_partial_cells(cells, used_cell_count);
    free(nulls);

    if (columns) {
        for (i = 0; i < col_count; i++) {
            free(columns[i]);
        }
        free(columns);
    }

    if (bound_values) {
        for (i = 0; i < col_count; i++) {
            free(bound_values[i]);
        }
        free(bound_values);
    }

    free(bound_lens);
    if (table) {
        mdb_free_tabledef(table);
    }
    return -1;
}

int mdbgo_query(mdbgo_db_t *db, const char *sql_text, mdbgo_table_data_t *out, char *err, size_t err_len) {
    MdbSQL *sql = NULL;
    char **columns = NULL;
    char **cells = NULL;
    size_t col_count = 0;
    size_t row_count = 0;
    size_t row_cap = 0;
    size_t used_cell_count = 0;
    size_t i;

    if (!db || !db->mdb || !sql_text || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    /* 防止 SQL 语句把共享 DB 句柄主动断开。 */
    if (mdbgo_starts_with_keyword(sql_text, "connect") || mdbgo_starts_with_keyword(sql_text, "disconnect")) {
        mdbgo_set_error(err, err_len, "connect/disconnect is not allowed in Query()");
        return -1;
    }

    memset(out, 0, sizeof(*out));

    sql = mdb_sql_init();
    if (!sql) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }
    sql->mdb = db->mdb;

    if (!mdb_sql_run_query(sql, sql_text) || mdb_sql_has_error(sql)) {
        mdbgo_set_error(err, err_len, "%s", mdb_sql_last_error(sql));
        goto fail;
    }

    if (!sql->cur_table) {
        mdbgo_set_error(err, err_len, "query returned no result table");
        goto fail;
    }

    col_count = (size_t)sql->num_columns;
    columns = (char **)calloc(col_count, sizeof(char *));
    if (!columns && col_count > 0) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }

    for (i = 0; i < col_count; i++) {
        MdbSQLColumn *sql_col = g_ptr_array_index(sql->columns, (guint)i);
        columns[i] = strdup((sql_col && sql_col->name) ? sql_col->name : "");
        if (!columns[i]) {
            mdbgo_set_error(err, err_len, "out of memory");
            goto fail;
        }
    }

    while (mdb_sql_fetch_row(sql, sql->cur_table)) {
        size_t base;

        if (row_count == row_cap) {
            size_t new_cap = (row_cap == 0) ? 64 : (row_cap * 2);
            char **new_cells = (char **)realloc(cells, new_cap * col_count * sizeof(char *));
            if (!new_cells) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            cells = new_cells;
            row_cap = new_cap;
        }

        base = row_count * col_count;
        for (i = 0; i < col_count; i++) {
            const char *src = (const char *)g_ptr_array_index(sql->bound_values, (guint)i);
            char *cell = strdup(src ? src : "");
            if (!cell) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            cells[base + i] = cell;
            used_cell_count++;
        }

        row_count++;
    }

    out->columns = columns;
    out->col_count = col_count;
    out->cells = cells;
    out->row_count = row_count;

    /* 避免 mdb_sql_exit 关闭共享句柄。 */
    sql->mdb = NULL;
    mdb_sql_exit(sql);
    return 0;

fail:
    mdbgo_free_partial_cells(cells, used_cell_count);
    mdbgo_free_partial_string_array(columns, col_count);
    if (sql) {
        sql->mdb = NULL;
        mdb_sql_exit(sql);
    }
    return -1;
}

void mdbgo_free_table_data(mdbgo_table_data_t *data) {
    size_t i;
    size_t total;

    if (!data) {
        return;
    }

    if (data->columns) {
        for (i = 0; i < data->col_count; i++) {
            free(data->columns[i]);
        }
        free(data->columns);
    }

    total = data->row_count * data->col_count;
    if (data->cells) {
        for (i = 0; i < total; i++) {
            free(data->cells[i]);
        }
        free(data->cells);
    }
    free(data->nulls);

    memset(data, 0, sizeof(*data));
}

/* 深拷贝一个 MdbProperties 到 mdbgo_property_block_t。 */
static int mdbgo_copy_property_block(const MdbProperties *src, mdbgo_property_block_t *dst) {
    size_t item_count = 0;
    mdbgo_hash_copy_ctx_t ctx;

    if (!dst) {
        return -1;
    }

    memset(dst, 0, sizeof(*dst));
    if (!src) {
        return 0;
    }

    dst->name = strdup(src->name ? src->name : "");
    if (!dst->name) {
        return -1;
    }

    if (!src->hash) {
        return 0;
    }

    g_hash_table_foreach(src->hash, mdbgo_hash_count_cb, &item_count);
    if (item_count == 0) {
        return 0;
    }

    dst->items = (mdbgo_property_item_t *)calloc(item_count, sizeof(mdbgo_property_item_t));
    if (!dst->items) {
        return -1;
    }
    dst->item_count = item_count;

    memset(&ctx, 0, sizeof(ctx));
    ctx.items = dst->items;
    ctx.item_count = item_count;
    ctx.index = 0;
    ctx.ok = 1;
    g_hash_table_foreach(src->hash, mdbgo_hash_copy_cb, &ctx);

    if (!ctx.ok) {
        return -1;
    }
    return 0;
}

int mdbgo_export_forms(mdbgo_db_t *db, mdbgo_forms_data_t *out, char *err, size_t err_len) {
    GPtrArray *catalog;
    mdbgo_form_info_t *forms = NULL;
    size_t form_count = 0;
    guint i;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    memset(out, 0, sizeof(*out));

    /*
     * 不能直接用 MDB_FORM 过滤：
     * 某些 Access 文件中 MSysObjects.Type 可能带额外标志位，
     * 在 catalog.c 的“type == objtype”判断阶段会被过滤掉。
     * 这里先读取 MDB_ANY，再按归一化后的 object_type 过滤。
     */
    catalog = mdb_read_catalog(db->mdb, MDB_ANY);
    if (!catalog) {
        mdbgo_set_error(err, err_len, "failed to read form catalog");
        return -1;
    }

    if (catalog->len > 0) {
        forms = (mdbgo_form_info_t *)calloc((size_t)catalog->len, sizeof(mdbgo_form_info_t));
        if (!forms) {
            mdbgo_set_error(err, err_len, "out of memory");
            return -1;
        }
    }

    for (i = 0; i < catalog->len; i++) {
        MdbCatalogEntry *entry = (MdbCatalogEntry *)g_ptr_array_index(catalog, i);
        mdbgo_form_info_t *dst;
        const char *obj_type_name;
        guint j;

        if (!entry) {
            continue;
        }

        if (entry->object_type != MDB_FORM) {
            continue;
        }

        dst = &forms[form_count];
        memset(dst, 0, sizeof(*dst));

        {
            const char *form_name = entry->object_name;
            if (form_name[0] == '\0') {
                form_name = "";
            }
            dst->name = strdup(form_name);
        }
        obj_type_name = mdb_get_objtype_string(entry->object_type);
        dst->object_type_name = strdup(obj_type_name ? obj_type_name : "Unknown");
        if (!dst->name || !dst->object_type_name) {
            mdbgo_set_error(err, err_len, "out of memory");
            goto fail;
        }

        dst->object_type = entry->object_type;
        dst->table_pg = entry->table_pg;
        dst->flags = entry->flags;

        if (entry->props && entry->props->len > 0) {
            dst->prop_blocks = (mdbgo_property_block_t *)calloc((size_t)entry->props->len, sizeof(mdbgo_property_block_t));
            if (!dst->prop_blocks) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            dst->prop_block_count = entry->props->len;

            for (j = 0; j < entry->props->len; j++) {
                MdbProperties *src_props = (MdbProperties *)g_ptr_array_index(entry->props, j);
                if (mdbgo_copy_property_block(src_props, &dst->prop_blocks[j]) != 0) {
                    mdbgo_set_error(err, err_len, "out of memory");
                    goto fail;
                }
            }
        }

        form_count++;
    }

    out->forms = forms;
    out->form_count = form_count;
    return 0;

fail:
    if (forms) {
        size_t k;
        for (k = 0; k <= form_count; k++) {
            mdbgo_free_form_info(&forms[k]);
        }
        free(forms);
    }
    return -1;
}

static int mdbgo_copy_table_schema(MdbTableDef *table, const char *table_name, mdbgo_table_schema_t *out, char *err, size_t err_len) {
    mdbgo_column_schema_t *cols = NULL;
    size_t i;
    size_t col_count;

    if (!table || !table_name || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    memset(out, 0, sizeof(*out));

    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read columns: %s", table_name);
        return -1;
    }

    col_count = table->num_cols;
    cols = (mdbgo_column_schema_t *)calloc(col_count, sizeof(mdbgo_column_schema_t));
    if (!cols && col_count > 0) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }

    out->table_name = strdup(table_name);
    if (!out->table_name) {
        mdbgo_set_error(err, err_len, "out of memory");
        free(cols);
        return -1;
    }

    out->columns = cols;
    out->col_count = col_count;
    out->row_count = table->num_rows;

    for (i = 0; i < col_count; i++) {
        MdbColumn *col = g_ptr_array_index(table->columns, (guint)i);
        const char *type_name = mdbgo_col_type_name(col ? col->col_type : -1);

        if (col) {
            cols[i].name = strdup(col->name);
        } else {
            cols[i].name = strdup("");
        }
        cols[i].col_type_name = strdup(type_name);
        if (!cols[i].name || !cols[i].col_type_name) {
            mdbgo_set_error(err, err_len, "out of memory");
            mdbgo_free_table_schema(out);
            return -1;
        }

        cols[i].col_type = col ? col->col_type : -1;
        cols[i].col_size = col ? col->col_size : 0;
        cols[i].col_prec = col ? col->col_prec : 0;
        cols[i].col_scale = col ? col->col_scale : 0;
        cols[i].is_fixed = col ? col->is_fixed : 0;
    }

    return 0;
}

int mdbgo_get_table_schema(mdbgo_db_t *db, const char *table_name, mdbgo_table_schema_t *out, char *err, size_t err_len) {
    MdbTableDef *table;
    int rc;

    if (!db || !db->mdb || !table_name || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    table = mdb_read_table_by_name(db->mdb, (gchar *)table_name, MDB_TABLE);
    if (!table) {
        mdbgo_set_error(err, err_len, "table not found: %s", table_name);
        return -1;
    }

    rc = mdbgo_copy_table_schema(table, table_name, out, err, err_len);
    mdb_free_tabledef(table);
    return rc;
}

void mdbgo_free_table_schema(mdbgo_table_schema_t *schema) {
    size_t i;

    if (!schema) {
        return;
    }

    free(schema->table_name);
    schema->table_name = NULL;

    if (schema->columns) {
        for (i = 0; i < schema->col_count; i++) {
            free(schema->columns[i].name);
            free(schema->columns[i].col_type_name);
        }
        free(schema->columns);
    }

    schema->columns = NULL;
    schema->col_count = 0;
    schema->row_count = 0;
}

int mdbgo_get_table_schemas(mdbgo_db_t *db, mdbgo_table_schemas_t *out, char *err, size_t err_len) {
    GPtrArray *catalog;
    mdbgo_table_schema_t *schemas;
    size_t count = 0;
    guint i;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    memset(out, 0, sizeof(*out));
    catalog = mdb_read_catalog(db->mdb, MDB_TABLE);
    if (!catalog) {
        mdbgo_set_error(err, err_len, "failed to read catalog");
        return -1;
    }

    schemas = (mdbgo_table_schema_t *)calloc((size_t)catalog->len, sizeof(mdbgo_table_schema_t));
    if (!schemas && catalog->len > 0) {
        mdbgo_set_error(err, err_len, "out of memory");
        return -1;
    }

    for (i = 0; i < catalog->len; i++) {
        MdbCatalogEntry *entry = g_ptr_array_index(catalog, i);
        MdbTableDef *table;

        if (!entry || !mdb_is_user_table(entry)) {
            continue;
        }

        table = mdb_read_table(entry);
        if (!table) {
            mdbgo_set_error(err, err_len, "failed to read table: %s", entry->object_name);
            goto fail;
        }
        if (mdbgo_copy_table_schema(table, entry->object_name, &schemas[count], err, err_len) != 0) {
            mdb_free_tabledef(table);
            goto fail;
        }
        mdb_free_tabledef(table);
        count++;
    }

    out->schemas = schemas;
    out->count = count;
    return 0;

fail:
    for (i = 0; i < count; i++) {
        mdbgo_free_table_schema(&schemas[i]);
    }
    free(schemas);
    return -1;
}

void mdbgo_free_table_schemas(mdbgo_table_schemas_t *schemas) {
    size_t i;

    if (!schemas) {
        return;
    }

    for (i = 0; i < schemas->count; i++) {
        mdbgo_free_table_schema(&schemas->schemas[i]);
    }
    free(schemas->schemas);
    schemas->schemas = NULL;
    schemas->count = 0;
}

int mdbgo_read_form_streams(mdbgo_db_t *db, const char *form_name, mdbgo_form_streams_t *out, char *err, size_t err_len) {
    MdbCatalogEntry msysobj;
    MdbTableDef *table = NULL;
    char *name_buf = NULL;
    char *lv_buf = NULL;
    char *lv_prop_buf = NULL;
    char *lv_extra_buf = NULL;
    int lv_len_dummy = 0;
    int lv_prop_len_dummy = 0;
    int lv_extra_len_dummy = 0;
    MdbColumn *col_lv = NULL;
    MdbColumn *col_lv_prop = NULL;
    MdbColumn *col_lv_extra = NULL;
    int idx_lv;
    int idx_lv_prop;
    int idx_lv_extra;
    size_t ole_len = 0;
    void *ole = NULL;
    int row_seen = 0;
    int first_id = 0;
    int has_first_id = 0;

    if (!db || !db->mdb || !form_name || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }

    memset(out, 0, sizeof(*out));
    if (form_name[0] == '\0') {
        mdbgo_set_error(err, err_len, "form name is empty");
        return -1;
    }

    memset(&msysobj, 0, sizeof(msysobj));
    msysobj.mdb = db->mdb;
    msysobj.object_type = MDB_TABLE;
    msysobj.table_pg = 2;
    snprintf(msysobj.object_name, sizeof(msysobj.object_name), "%s", "MSysObjects");

    table = mdb_read_table(&msysobj);
    if (!table) {
        mdbgo_set_error(err, err_len, "failed to read MSysObjects");
        goto fail;
    }
    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read MSysObjects columns");
        goto fail;
    }

    name_buf = (char *)calloc(1, db->mdb->bind_size);
    lv_buf = (char *)calloc(1, db->mdb->bind_size);
    lv_prop_buf = (char *)calloc(1, db->mdb->bind_size);
    lv_extra_buf = (char *)calloc(1, db->mdb->bind_size);
    if (!name_buf || !lv_buf || !lv_prop_buf || !lv_extra_buf) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }

    if (mdb_bind_column_by_name(table, "Name", name_buf, NULL) == -1) {
        mdbgo_set_error(err, err_len, "failed to bind Name");
        goto fail;
    }

    idx_lv = mdb_bind_column_by_name(table, "Lv", lv_buf, &lv_len_dummy);
    idx_lv_prop = mdb_bind_column_by_name(table, "LvProp", lv_prop_buf, &lv_prop_len_dummy);
    idx_lv_extra = mdb_bind_column_by_name(table, "LvExtra", lv_extra_buf, &lv_extra_len_dummy);
    if (idx_lv == -1 || idx_lv_prop == -1 || idx_lv_extra == -1) {
        mdbgo_set_error(err, err_len, "failed to bind form stream columns");
        goto fail;
    }

    col_lv = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_lv - 1));
    col_lv_prop = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_lv_prop - 1));
    col_lv_extra = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_lv_extra - 1));
    if (!col_lv || !col_lv_prop || !col_lv_extra) {
        mdbgo_set_error(err, err_len, "invalid form stream column metadata");
        goto fail;
    }

    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        if (g_ascii_strcasecmp(name_buf, form_name) != 0) {
            continue;
        }

        out->form_name = strdup(form_name);
        if (!out->form_name) {
            mdbgo_set_error(err, err_len, "out of memory");
            goto fail;
        }

        ole = mdb_ole_read_full(db->mdb, col_lv, &ole_len);
        if (ole) {
            if (mdbgo_copy_stream_bytes(ole, ole_len, &out->lv, &out->lv_len) != 0) {
                free(ole);
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            free(ole);
            ole = NULL;
        } else if (lv_len_dummy > 0) {
            /* 某些版本/对象中，OLE 列可能以内嵌值返回，读链路会失败。 */
            if (mdbgo_copy_stream_bytes(lv_buf, (size_t)lv_len_dummy, &out->lv, &out->lv_len) != 0) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
        }

        ole_len = 0;
        ole = mdb_ole_read_full(db->mdb, col_lv_prop, &ole_len);
        if (ole) {
            if (mdbgo_copy_stream_bytes(ole, ole_len, &out->lv_prop, &out->lv_prop_len) != 0) {
                free(ole);
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            free(ole);
            ole = NULL;
        } else if (lv_prop_len_dummy > 0) {
            if (mdbgo_copy_stream_bytes(lv_prop_buf, (size_t)lv_prop_len_dummy, &out->lv_prop, &out->lv_prop_len) != 0) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
        }

        ole_len = 0;
        ole = mdb_ole_read_full(db->mdb, col_lv_extra, &ole_len);
        if (ole) {
            if (mdbgo_copy_stream_bytes(ole, ole_len, &out->lv_extra, &out->lv_extra_len) != 0) {
                free(ole);
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            free(ole);
            ole = NULL;
        } else if (lv_extra_len_dummy > 0) {
            if (mdbgo_copy_stream_bytes(lv_extra_buf, (size_t)lv_extra_len_dummy, &out->lv_extra, &out->lv_extra_len) != 0) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
        }

        free(name_buf);
        free(lv_buf);
        free(lv_prop_buf);
        free(lv_extra_buf);
        mdb_free_tabledef(table);
        return 0;
    }

    mdbgo_set_error(err, err_len, "form not found: %s", form_name);

fail:
    free(ole);
    free(name_buf);
    free(lv_buf);
    free(lv_prop_buf);
    free(lv_extra_buf);
    if (table) {
        mdb_free_tabledef(table);
    }
    mdbgo_free_form_streams_inner(out);
    return -1;
}

void mdbgo_free_forms_data(mdbgo_forms_data_t *data) {
    size_t i;

    if (!data) {
        return;
    }

    if (data->forms) {
        for (i = 0; i < data->form_count; i++) {
            mdbgo_free_form_info(&data->forms[i]);
        }
        free(data->forms);
    }

    data->forms = NULL;
    data->form_count = 0;
}

void mdbgo_free_form_streams(mdbgo_form_streams_t *data) {
    mdbgo_free_form_streams_inner(data);
}

int mdbgo_read_access_object_data_by_id(mdbgo_db_t *db, int object_id, mdbgo_blob_data_t *out, char *err, size_t err_len) {
    MdbTableDef *table = NULL;
    char *id_buf = NULL;
    int data_len_dummy = 0;
    int idx_data;
    MdbColumn *col_data = NULL;
    size_t ole_len = 0;
    void *ole = NULL;
    int row_seen = 0;
    int first_id = 0;
    int has_first_id = 0;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    memset(out, 0, sizeof(*out));

    table = mdb_read_table_by_name(db->mdb, (gchar *)"MSysAccessObjects", MDB_ANY);
    if (!table) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects");
        goto fail;
    }
    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects columns");
        goto fail;
    }

    id_buf = (char *)calloc(1, db->mdb->bind_size);
    if (!id_buf) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }

    if (mdb_bind_column(table, 2, id_buf, NULL) == -1) {
        mdbgo_set_error(err, err_len, "failed to bind ID (column 2)");
        goto fail;
    }
    /* Data(type=17) 不做字符串绑定，避免触发 mdb_col_to_string 的未知类型转换。 */
    idx_data = mdb_bind_column(table, 1, NULL, &data_len_dummy);
    if (idx_data == -1) {
        mdbgo_set_error(err, err_len, "failed to bind Data (column 1)");
        goto fail;
    }

    col_data = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_data - 1));
    if (!col_data) {
        mdbgo_set_error(err, err_len, "invalid Data column metadata");
        goto fail;
    }

    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        int row_id = atoi(id_buf);
        if (!has_first_id) {
            first_id = row_id;
            has_first_id = 1;
        }
        row_seen++;
        if (row_id != object_id) {
            continue;
        }

        /*
         * 优先拷贝当前行原始列字节：
         * 对于未知类型（如 17）mdb_col_to_string 无法处理，但 cur_value_* 仍有效。
         */
        if (col_data->cur_value_len > 0 && col_data->cur_value_start >= 0 &&
            (size_t)(col_data->cur_value_start + col_data->cur_value_len) <= db->mdb->fmt->pg_size) {
            if (mdbgo_copy_stream_bytes(
                    db->mdb->pg_buf + col_data->cur_value_start,
                    (size_t)col_data->cur_value_len,
                    &out->data,
                    &out->len) != 0) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
        } else {
            /* 再尝试 OLE 链式读取（兼容某些对象流存储）。 */
            ole = mdb_ole_read_full(db->mdb, col_data, &ole_len);
            if (ole && ole_len > 0) {
                if (mdbgo_copy_stream_bytes(ole, ole_len, &out->data, &out->len) != 0) {
                    mdbgo_set_error(err, err_len, "out of memory");
                    goto fail;
                }
                free(ole);
                ole = NULL;
            } else {
                out->data = NULL;
                out->len = 0;
            }
        }

        if (out->len == 0) {
            mdbgo_set_error(
                err,
                err_len,
                "access object data is empty for id=%d (cur_start=%d cur_len=%d bind_len=%d col_type=%d)",
                object_id,
                col_data->cur_value_start,
                col_data->cur_value_len,
                data_len_dummy,
                col_data->col_type);
            goto fail;
        }

        free(ole);
        free(id_buf);
        mdb_free_tabledef(table);
        return 0;
    }

    if (has_first_id) {
        mdbgo_set_error(err, err_len, "access object id not found: %d (rows=%d first_id=%d)", object_id, row_seen, first_id);
    } else {
        mdbgo_set_error(err, err_len, "access object id not found: %d (rows=%d)", object_id, row_seen);
    }

fail:
    free(ole);
    free(id_buf);
    if (table) {
        mdb_free_tabledef(table);
    }
    mdbgo_free_blob_data_inner(out);
    return -1;
}

void mdbgo_free_blob_data(mdbgo_blob_data_t *out) {
    mdbgo_free_blob_data_inner(out);
}

int mdbgo_read_access_object_data_all(mdbgo_db_t *db, mdbgo_access_object_data_array_t *out, char *err, size_t err_len) {
    MdbTableDef *table = NULL;
    char *id_buf = NULL;
    int data_len_dummy = 0;
    int idx_data;
    MdbColumn *col_data = NULL;
    mdbgo_access_object_data_t *values = NULL;
    size_t count = 0;
    size_t cap = 0;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    memset(out, 0, sizeof(*out));

    table = mdb_read_table_by_name(db->mdb, (gchar *)"MSysAccessObjects", MDB_ANY);
    if (!table) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects");
        goto fail;
    }
    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects columns");
        goto fail;
    }

    id_buf = (char *)calloc(1, db->mdb->bind_size);
    if (!id_buf) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }
    if (mdb_bind_column(table, 2, id_buf, NULL) == -1) {
        mdbgo_set_error(err, err_len, "failed to bind ID (column 2)");
        goto fail;
    }
    idx_data = mdb_bind_column(table, 1, NULL, &data_len_dummy);
    if (idx_data == -1) {
        mdbgo_set_error(err, err_len, "failed to bind Data (column 1)");
        goto fail;
    }
    col_data = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_data - 1));
    if (!col_data) {
        mdbgo_set_error(err, err_len, "invalid Data column metadata");
        goto fail;
    }

    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        mdbgo_access_object_data_t *item;
        size_t ole_len = 0;
        void *ole = NULL;

        if (count == cap) {
            size_t new_cap = (cap == 0) ? 32 : (cap * 2);
            mdbgo_access_object_data_t *new_values =
                (mdbgo_access_object_data_t *)realloc(values, new_cap * sizeof(*values));
            if (!new_values) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            values = new_values;
            cap = new_cap;
        }

        item = &values[count];
        memset(item, 0, sizeof(*item));
        item->object_id = atoi(id_buf);
        count++;

        if (col_data->cur_value_len > 0 && col_data->cur_value_start >= 0 &&
            (size_t)(col_data->cur_value_start + col_data->cur_value_len) <= db->mdb->fmt->pg_size) {
            if (mdbgo_copy_stream_bytes(
                    db->mdb->pg_buf + col_data->cur_value_start,
                    (size_t)col_data->cur_value_len,
                    &item->data,
                    &item->len) != 0) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
        } else {
            ole = mdb_ole_read_full(db->mdb, col_data, &ole_len);
            if (ole && ole_len > 0 &&
                mdbgo_copy_stream_bytes(ole, ole_len, &item->data, &item->len) != 0) {
                free(ole);
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            free(ole);
        }

        if (item->len == 0) {
            mdbgo_set_error(
                err,
                err_len,
                "access object data is empty for id=%d (cur_start=%d cur_len=%d bind_len=%d col_type=%d)",
                item->object_id,
                col_data->cur_value_start,
                col_data->cur_value_len,
                data_len_dummy,
                col_data->col_type);
            goto fail;
        }
    }

    free(id_buf);
    mdb_free_tabledef(table);
    out->values = values;
    out->count = count;
    return 0;

fail:
    free(id_buf);
    if (table) {
        mdb_free_tabledef(table);
    }
    out->values = values;
    out->count = count;
    mdbgo_free_access_object_data_array_inner(out);
    return -1;
}

void mdbgo_free_access_object_data_array(mdbgo_access_object_data_array_t *out) {
    mdbgo_free_access_object_data_array_inner(out);
}

int mdbgo_access_object_storage_kind(mdbgo_db_t *db, int *out_kind, char *err, size_t err_len) {
    GPtrArray *catalog;
    int has_access_storage = 0;
    guint i;

    if (!db || !db->mdb || !out_kind) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    *out_kind = 0;

    catalog = mdb_read_catalog(db->mdb, MDB_ANY);
    if (!catalog) {
        mdbgo_set_error(err, err_len, "failed to read catalog");
        return -1;
    }
    for (i = 0; i < catalog->len; i++) {
        MdbCatalogEntry *entry = (MdbCatalogEntry *)g_ptr_array_index(catalog, i);
        if (!entry) {
            continue;
        }
        if (!g_ascii_strcasecmp(entry->object_name, "MSysAccessObjects")) {
            *out_kind = 1;
            return 0;
        }
        if (!g_ascii_strcasecmp(entry->object_name, "MSysAccessStorage")) {
            has_access_storage = 1;
        }
    }
    if (has_access_storage) {
        *out_kind = 2;
    }
    return 0;
}

int mdbgo_read_access_storage_entries(
    mdbgo_db_t *db,
    mdbgo_access_storage_entry_array_t *out,
    char *err,
    size_t err_len
) {
    MdbTableDef *table = NULL;
    MdbColumn *col_lv = NULL;
    mdbgo_access_storage_entry_t *values = NULL;
    char *id_buf = NULL;
    char *parent_id_buf = NULL;
    char *type_buf = NULL;
    char *name_buf = NULL;
    unsigned char *lv_buf = NULL;
    int lv_len_dummy = 0;
    int idx_lv;
    size_t count = 0;
    size_t cap = 0;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    memset(out, 0, sizeof(*out));

    table = mdb_read_table_by_name(db->mdb, (gchar *)"MSysAccessStorage", MDB_ANY);
    if (!table) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessStorage");
        goto fail;
    }
    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessStorage columns");
        goto fail;
    }

    id_buf = (char *)calloc(1, db->mdb->bind_size);
    parent_id_buf = (char *)calloc(1, db->mdb->bind_size);
    type_buf = (char *)calloc(1, db->mdb->bind_size);
    name_buf = (char *)calloc(1, db->mdb->bind_size);
    lv_buf = (unsigned char *)calloc(1, db->mdb->bind_size);
    if (!id_buf || !parent_id_buf || !type_buf || !name_buf || !lv_buf) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }

    if (mdb_bind_column_by_name(table, (gchar *)"Id", id_buf, NULL) == -1 ||
        mdb_bind_column_by_name(table, (gchar *)"ParentId", parent_id_buf, NULL) == -1 ||
        mdb_bind_column_by_name(table, (gchar *)"Type", type_buf, NULL) == -1 ||
        mdb_bind_column_by_name(table, (gchar *)"Name", name_buf, NULL) == -1) {
        mdbgo_set_error(err, err_len, "failed to bind MSysAccessStorage metadata columns");
        goto fail;
    }
    idx_lv = mdb_bind_column_by_name(table, (gchar *)"Lv", lv_buf, &lv_len_dummy);
    if (idx_lv == -1) {
        mdbgo_set_error(err, err_len, "failed to bind MSysAccessStorage Lv");
        goto fail;
    }
    col_lv = (MdbColumn *)g_ptr_array_index(table->columns, (guint)(idx_lv - 1));
    if (!col_lv || col_lv->col_type != MDB_OLE) {
        mdbgo_set_error(err, err_len, "invalid MSysAccessStorage Lv column metadata");
        goto fail;
    }

    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        mdbgo_access_storage_entry_t *item;

        if (count == cap) {
            size_t new_cap = (cap == 0) ? 128 : (cap * 2);
            mdbgo_access_storage_entry_t *new_values =
                (mdbgo_access_storage_entry_t *)realloc(values, new_cap * sizeof(*values));
            if (!new_values) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            values = new_values;
            cap = new_cap;
        }

        item = &values[count];
        memset(item, 0, sizeof(*item));
        item->id = atoi(id_buf);
        item->parent_id = atoi(parent_id_buf);
        item->entry_type = atoi(type_buf);
        item->name = strdup(name_buf);
        if (!item->name) {
            mdbgo_set_error(err, err_len, "out of memory");
            goto fail;
        }
        count++;

        if (!col_lv->is_null && col_lv->cur_value_len > 0) {
            size_t ole_len = 0;
            void *ole;

            if (col_lv->cur_value_len < MDB_MEMO_OVERHEAD) {
                mdbgo_set_error(
                    err,
                    err_len,
                    "invalid MSysAccessStorage Lv value for id=%d: len=%d",
                    item->id,
                    col_lv->cur_value_len);
                goto fail;
            }
            ole = mdb_ole_read_full(db->mdb, col_lv, &ole_len);
            if (!ole) {
                mdbgo_set_error(err, err_len, "failed to read MSysAccessStorage Lv for id=%d", item->id);
                goto fail;
            }
            if (ole_len > 0) {
                item->data = (unsigned char *)ole;
                item->len = ole_len;
            } else {
                free(ole);
            }
        }
    }

    free(id_buf);
    free(parent_id_buf);
    free(type_buf);
    free(name_buf);
    free(lv_buf);
    mdb_free_tabledef(table);
    out->values = values;
    out->count = count;
    return 0;

fail:
    free(id_buf);
    free(parent_id_buf);
    free(type_buf);
    free(name_buf);
    free(lv_buf);
    if (table) {
        mdb_free_tabledef(table);
    }
    out->values = values;
    out->count = count;
    mdbgo_free_access_storage_entry_array_inner(out);
    return -1;
}

void mdbgo_free_access_storage_entries(mdbgo_access_storage_entry_array_t *out) {
    mdbgo_free_access_storage_entry_array_inner(out);
}

int mdbgo_list_access_object_ids(mdbgo_db_t *db, mdbgo_int_array_t *out, char *err, size_t err_len) {
    MdbTableDef *table = NULL;
    char *id_buf = NULL;
    int *ids = NULL;
    size_t count = 0;
    size_t cap = 0;

    if (!db || !db->mdb || !out) {
        mdbgo_set_error(err, err_len, "invalid arguments");
        return -1;
    }
    memset(out, 0, sizeof(*out));

    table = mdb_read_table_by_name(db->mdb, (gchar *)"MSysAccessObjects", MDB_ANY);
    if (!table) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects");
        goto fail;
    }
    if (!mdb_read_columns(table)) {
        mdbgo_set_error(err, err_len, "failed to read MSysAccessObjects columns");
        goto fail;
    }

    id_buf = (char *)calloc(1, db->mdb->bind_size);
    if (!id_buf) {
        mdbgo_set_error(err, err_len, "out of memory");
        goto fail;
    }
    if (mdb_bind_column(table, 2, id_buf, NULL) == -1) {
        mdbgo_set_error(err, err_len, "failed to bind ID (column 2)");
        goto fail;
    }

    mdb_rewind_table(table);
    while (mdb_fetch_row(table)) {
        int v;
        if (count == cap) {
            size_t new_cap = (cap == 0) ? 32 : (cap * 2);
            int *new_ids = (int *)realloc(ids, new_cap * sizeof(int));
            if (!new_ids) {
                mdbgo_set_error(err, err_len, "out of memory");
                goto fail;
            }
            ids = new_ids;
            cap = new_cap;
        }
        v = atoi(id_buf);
        ids[count++] = v;
    }

    free(id_buf);
    mdb_free_tabledef(table);
    out->values = ids;
    out->count = count;
    return 0;

fail:
    free(id_buf);
    if (table) {
        mdb_free_tabledef(table);
    }
    free(ids);
    mdbgo_free_int_array_inner(out);
    return -1;
}

void mdbgo_free_int_array(mdbgo_int_array_t *out) {
    mdbgo_free_int_array_inner(out);
}

/* View/保存查询支持单独维护，并与 bridge.c 处于同一编译单元。 */
#include "view.c"
