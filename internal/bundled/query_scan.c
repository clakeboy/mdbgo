#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "bridge.h"
#include "include/mdbtools.h"

/* bridge.c keeps this definition private; repeat the compatible definition. */
struct mdbgo_db {
    MdbHandle *mdb;
};

typedef struct mdbgo_scan {
    MdbTableDef *table;
    char **bound_values;
    int *bound_lens;
    int *column_numbers;
    char **column_names;
    size_t col_count;
    int exhausted;
} mdbgo_scan_t;

static void scan_set_error(char *err, size_t err_len, const char *fmt, ...) {
    va_list ap;
    if (!err || err_len == 0) {
        return;
    }
    va_start(ap, fmt);
    vsnprintf(err, err_len, fmt, ap);
    va_end(ap);
    err[err_len - 1] = '\0';
}

static void scan_free_partial_data(mdbgo_table_data_t *data) {
    size_t i;
    size_t total;
    if (!data) {
        return;
    }
    for (i = 0; i < data->col_count; i++) {
        free(data->columns ? data->columns[i] : NULL);
    }
    free(data->columns);
    total = data->row_count * data->col_count;
    for (i = 0; i < total; i++) {
        free(data->cells ? data->cells[i] : NULL);
    }
    free(data->cells);
    free(data->nulls);
    memset(data, 0, sizeof(*data));
}

static void scan_close_inner(mdbgo_scan_t *scan) {
    size_t i;
    if (!scan) {
        return;
    }
    if (scan->bound_values) {
        for (i = 0; i < scan->col_count; i++) {
            free(scan->bound_values[i]);
        }
    }
    if (scan->column_names) {
        for (i = 0; i < scan->col_count; i++) {
            free(scan->column_names[i]);
        }
    }
    free(scan->bound_values);
    free(scan->bound_lens);
    free(scan->column_numbers);
    free(scan->column_names);
    if (scan->table) {
        mdb_index_scan_free(scan->table);
        mdb_free_tabledef(scan->table);
    }
    free(scan);
}

int mdbgo_scan_open(
    mdbgo_db_t *db,
    const char *table_name,
    const char *const *requested_columns,
    size_t requested_count,
    mdbgo_scan_t **out_scan,
    char *err,
    size_t err_len
) {
    mdbgo_scan_t *scan = NULL;
    size_t count;
    size_t i;

    if (!db || !db->mdb || !table_name || !out_scan) {
        scan_set_error(err, err_len, "invalid scan arguments");
        return -1;
    }
    *out_scan = NULL;
    scan = (mdbgo_scan_t *)calloc(1, sizeof(*scan));
    if (!scan) {
        scan_set_error(err, err_len, "out of memory");
        return -1;
    }
    scan->table = mdb_read_table_by_name(db->mdb, (gchar *)table_name, MDB_TABLE);
    if (!scan->table || !mdb_read_columns(scan->table)) {
        scan_set_error(err, err_len, "failed to read table: %s", table_name);
        scan_close_inner(scan);
        return -1;
    }
    count = requested_count ? requested_count : scan->table->num_cols;
    scan->bound_values = (char **)calloc(count, sizeof(char *));
    scan->bound_lens = (int *)calloc(count, sizeof(int));
    scan->column_numbers = (int *)calloc(count, sizeof(int));
    scan->column_names = (char **)calloc(count, sizeof(char *));
    if ((!scan->bound_values || !scan->bound_lens || !scan->column_numbers || !scan->column_names) && count > 0) {
        scan_set_error(err, err_len, "out of memory");
        scan_close_inner(scan);
        return -1;
    }
    scan->col_count = count;
    for (i = 0; i < count; i++) {
        int column_number = -1;
        MdbColumn *column = NULL;
        unsigned int j;
        if (requested_count) {
            for (j = 0; j < scan->table->num_cols; j++) {
                MdbColumn *candidate = (MdbColumn *)g_ptr_array_index(scan->table->columns, j);
                if (candidate && !g_ascii_strcasecmp(candidate->name, requested_columns[i])) {
                    column_number = (int)j + 1;
                    column = candidate;
                    break;
                }
            }
        } else {
            column_number = (int)i + 1;
            column = (MdbColumn *)g_ptr_array_index(scan->table->columns, (guint)i);
        }
        if (column_number < 1 || !column) {
            scan_set_error(err, err_len, "column not found in %s: %s", table_name, requested_columns[i]);
            scan_close_inner(scan);
            return -1;
        }
        scan->column_numbers[i] = column_number;
        scan->column_names[i] = strdup(column->name);
        scan->bound_values[i] = (char *)calloc(1, db->mdb->bind_size);
        if (!scan->column_names[i] || !scan->bound_values[i] ||
            mdb_bind_column(scan->table, column_number, scan->bound_values[i], &scan->bound_lens[i]) == -1) {
            scan_set_error(err, err_len, "failed to bind column: %s", column->name);
            scan_close_inner(scan);
            return -1;
        }
    }
    mdb_read_indices(scan->table);
    mdb_rewind_table(scan->table);
    mdb_index_scan_init(db->mdb, scan->table);
    *out_scan = scan;
    return 0;
}

int mdbgo_scan_next(
    mdbgo_scan_t *scan,
    size_t max_rows,
    mdbgo_table_data_t *out,
    char *err,
    size_t err_len
) {
    size_t row_count = 0;
    size_t i;
    if (!scan || !out || max_rows == 0) {
        scan_set_error(err, err_len, "invalid scan-next arguments");
        return -1;
    }
    memset(out, 0, sizeof(*out));
    out->col_count = scan->col_count;
    out->columns = (char **)calloc(scan->col_count, sizeof(char *));
    out->cells = (char **)calloc(max_rows * scan->col_count, sizeof(char *));
    out->nulls = (unsigned char *)calloc(max_rows * scan->col_count, sizeof(unsigned char));
    if ((!out->columns && scan->col_count > 0) || (!out->cells && scan->col_count > 0) ||
        (!out->nulls && scan->col_count > 0)) {
        scan_set_error(err, err_len, "out of memory");
        scan_free_partial_data(out);
        return -1;
    }
    for (i = 0; i < scan->col_count; i++) {
        out->columns[i] = strdup(scan->column_names[i]);
        if (!out->columns[i]) {
            scan_set_error(err, err_len, "out of memory");
            scan_free_partial_data(out);
            return -1;
        }
    }
    if (scan->exhausted) {
        return 0;
    }
    while (row_count < max_rows && mdb_fetch_row(scan->table)) {
        size_t base = row_count * scan->col_count;
        for (i = 0; i < scan->col_count; i++) {
            int len = scan->bound_lens[i];
            MdbColumn *column = (MdbColumn *)g_ptr_array_index(
                scan->table->columns, (guint)(scan->column_numbers[i] - 1));
            if (len < 0) {
                len = 0;
            }
            out->nulls[base + i] = column ? column->is_null : 0;
            out->cells[base + i] = (char *)malloc((size_t)len + 1);
            if (!out->cells[base + i]) {
                out->row_count = row_count + 1;
                scan_set_error(err, err_len, "out of memory");
                scan_free_partial_data(out);
                return -1;
            }
            if (len > 0) {
                memcpy(out->cells[base + i], scan->bound_values[i], (size_t)len);
            }
            out->cells[base + i][len] = '\0';
        }
        row_count++;
    }
    out->row_count = row_count;
    if (row_count < max_rows) {
        scan->exhausted = 1;
    }
    return 0;
}

void mdbgo_scan_close(mdbgo_scan_t *scan) {
    scan_close_inner(scan);
}
