#ifndef MDBGO_BRIDGE_H
#define MDBGO_BRIDGE_H

#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/* 不透明数据库句柄，避免把 libmdb 的内部结构暴露给 Go 层。 */
typedef struct mdbgo_db mdbgo_db_t;

/* 二维表数据（行主序）。cells 的长度是 row_count * col_count。 */
typedef struct mdbgo_table_data {
    char **columns;
    size_t col_count;
    char **cells;
    size_t row_count;
} mdbgo_table_data_t;

/* 单列 schema 信息。 */
typedef struct mdbgo_column_schema {
    char *name;
    int col_type;
    char *col_type_name;
    int col_size;
    int col_prec;
    int col_scale;
    int is_fixed;
} mdbgo_column_schema_t;

/* 表 schema 信息。 */
typedef struct mdbgo_table_schema {
    char *table_name;
    mdbgo_column_schema_t *columns;
    size_t col_count;
} mdbgo_table_schema_t;

/* 简单键值对，表示对象属性。 */
typedef struct mdbgo_property_item {
    char *key;
    char *value;
} mdbgo_property_item_t;

/* 一组属性块：name 为空表示窗体本身属性，否则通常对应某个组件。 */
typedef struct mdbgo_property_block {
    char *name;
    mdbgo_property_item_t *items;
    size_t item_count;
} mdbgo_property_block_t;

/* Access 窗体对象信息。 */
typedef struct mdbgo_form_info {
    char *name;
    int object_type;
    char *object_type_name;
    unsigned long table_pg;
    int flags;
    mdbgo_property_block_t *prop_blocks;
    size_t prop_block_count;
} mdbgo_form_info_t;

/* 多个窗体对象的导出结果。 */
typedef struct mdbgo_forms_data {
    mdbgo_form_info_t *forms;
    size_t form_count;
} mdbgo_forms_data_t;

/* 单个窗体的原始设计流。 */
typedef struct mdbgo_form_streams {
    char *form_name;
    unsigned char *lv;
    size_t lv_len;
    unsigned char *lv_prop;
    size_t lv_prop_len;
    unsigned char *lv_extra;
    size_t lv_extra_len;
} mdbgo_form_streams_t;

/* 原始字节缓冲。 */
typedef struct mdbgo_blob_data {
    unsigned char *data;
    size_t len;
} mdbgo_blob_data_t;

/* int 数组结果。 */
typedef struct mdbgo_int_array {
    int *values;
    size_t count;
} mdbgo_int_array_t;

/* 打开 MDB 文件。成功返回 0，失败返回非 0。 */
int mdbgo_open(const char *path, mdbgo_db_t **out_db, char *err, size_t err_len);

/* 关闭数据库句柄，允许传入 NULL。 */
void mdbgo_close(mdbgo_db_t *db);

/* 返回用户表名列表。names 需用 mdbgo_free_string_array 释放。 */
int mdbgo_list_tables(mdbgo_db_t *db, char ***out_names, size_t *out_count, char *err, size_t err_len);

/* 返回 Access 保存查询（View）名称列表。names 需用 mdbgo_free_string_array 释放。 */
int mdbgo_list_views(mdbgo_db_t *db, char ***out_names, size_t *out_count, char *err, size_t err_len);

/* 还原指定 Access 保存查询的 SQL。out_sql 需用 mdbgo_free_string 释放。 */
int mdbgo_get_view_sql(mdbgo_db_t *db, const char *view_name, char **out_sql, char *err, size_t err_len);

/* 按表名读取所有数据到内存。out 需用 mdbgo_free_table_data 释放。 */
int mdbgo_read_table(mdbgo_db_t *db, const char *table_name, mdbgo_table_data_t *out, char *err, size_t err_len);

/* 执行 SQL 并返回结果集（当前支持返回行集的查询）。out 需用 mdbgo_free_table_data 释放。 */
int mdbgo_query(mdbgo_db_t *db, const char *sql, mdbgo_table_data_t *out, char *err, size_t err_len);

/* 释放字符串数组（数组本身和每个元素）。 */
void mdbgo_free_string_array(char **arr, size_t count);

/* 释放 C bridge 返回的单个字符串。 */
void mdbgo_free_string(char *value);

/* 释放 mdbgo_read_table 返回的数据。 */
void mdbgo_free_table_data(mdbgo_table_data_t *data);

/* 读取表 schema。out 需用 mdbgo_free_table_schema 释放。 */
int mdbgo_get_table_schema(mdbgo_db_t *db, const char *table_name, mdbgo_table_schema_t *out, char *err, size_t err_len);

/* 释放 mdbgo_get_table_schema 返回的数据。 */
void mdbgo_free_table_schema(mdbgo_table_schema_t *schema);

/* 导出 Access 窗体和组件信息。out 需用 mdbgo_free_forms_data 释放。 */
int mdbgo_export_forms(mdbgo_db_t *db, mdbgo_forms_data_t *out, char *err, size_t err_len);

/* 释放 mdbgo_export_forms 返回的数据。 */
void mdbgo_free_forms_data(mdbgo_forms_data_t *data);

/* 读取窗体的原始设计流（Lv/LvProp/LvExtra）。out 需用 mdbgo_free_form_streams 释放。 */
int mdbgo_read_form_streams(mdbgo_db_t *db, const char *form_name, mdbgo_form_streams_t *out, char *err, size_t err_len);

/* 释放 mdbgo_read_form_streams 返回的数据。 */
void mdbgo_free_form_streams(mdbgo_form_streams_t *data);

/* 按 ID 读取 MSysAccessObjects.Data 原始字节。out 需用 mdbgo_free_blob_data 释放。 */
int mdbgo_read_access_object_data_by_id(mdbgo_db_t *db, int object_id, mdbgo_blob_data_t *out, char *err, size_t err_len);

/* 释放 mdbgo_read_access_object_data_by_id 返回的数据。 */
void mdbgo_free_blob_data(mdbgo_blob_data_t *out);

/* 列出 MSysAccessObjects 的全部 ID。out 需用 mdbgo_free_int_array 释放。 */
int mdbgo_list_access_object_ids(mdbgo_db_t *db, mdbgo_int_array_t *out, char *err, size_t err_len);

/* 释放 mdbgo_list_access_object_ids 返回的数据。 */
void mdbgo_free_int_array(mdbgo_int_array_t *out);

#ifdef __cplusplus
}
#endif

#endif
