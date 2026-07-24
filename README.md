# mdbgo (bundled mode)

`mdbgo` 是一个可直接被其它 Go 项目引用的 MDB 读取库。

- 使用 `bundled` 模式：构建时直接编译仓库内的 `libmdb` C 源码
- 调用方不需要额外安装系统级 `libmdb`
- 当前提供只读能力：打开数据库、列出表和 View、还原 View SQL、读取整张表、读取 Access 窗体内容
- 内置 Go 只读 SQL 引擎：支持参数、JOIN、聚合、排序、UNION、子查询、保存查询和分页

源码按职责拆分：`mdbgo.go` 只保留连接生命周期，`sql.go` 负责表和 SQL 数据读取，`form.go` 负责 Form 公共入口，`form_storage.go/form_layout.go` 负责内部存储与布局；各已实现的控件解析器位于独立的 `form_component_<type>.go` 文件中。

## 安装

```bash
go get github.com/clakeboy/mdbgo
```

## 快速示例

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/clakeboy/mdbgo"
)

func main() {
    db, err := mdbgo.Open("./example.mdb")
    if err != nil {
        log.Fatal(err)
    }
    defer db.Close()

    fmt.Printf("format=%s engine=%s pageSize=%d objectStorage=%s\n",
        db.Format.Name, db.Format.Engine, db.Format.PageSize, db.Format.ObjectStorage)

    tables, err := db.Tables()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("tables:", tables)

    views, err := db.Views()
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("views:", views)
    if len(views) > 0 {
        viewSQL, err := db.ViewSQL(views[0])
        if err != nil {
            log.Fatal(err)
        }
        fmt.Println(viewSQL)
    }

    if len(tables) == 0 {
        return
    }

    rowCount, err := db.TableRowCount(tables[0])
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("row count:", rowCount)

    data, err := db.ReadTable(tables[0])
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("columns:", data.Columns)
    fmt.Println("rows:", len(data.Rows))

    q, err := db.Query("SELECT * FROM [" + tables[0] + "] LIMIT 5")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("query rows:", len(q.Rows))

    typedRows, err := db.QueryContext(
        context.Background(),
        `SELECT TOP 100 a.*, Count(b.id) AS total
         FROM [orders] AS a LEFT JOIN [order_lines] AS b ON a.id=b.order_id
         WHERE a.created_at >= [from]
         GROUP BY a.id
         ORDER BY a.id`,
        map[string]any{"from": time.Now().AddDate(0, -1, 0)},
    )
    if err != nil {
        log.Fatal(err)
    }
    defer typedRows.Close()

    forms, err := db.ExportFormContents()
    if err != nil {
        log.Fatal(err)
    }
    for _, form := range forms {
        fmt.Printf("form=%s recordSource=%s sections=%d\n",
            form.FormName, form.RecordSource, len(form.Sections))
        for _, section := range form.Sections {
            fmt.Printf("  section=%s controls=%d\n", section.Type, len(section.Controls))
            for _, control := range section.Controls {
                fmt.Printf("    %s %s left=%d top=%d width=%d height=%d caption=%q source=%q\n",
                    control.Type, control.Name, control.Left, control.Top,
                    control.Width, control.Height, control.Caption, control.ControlSource)
            }
        }
    }
}
```

## API

- `Open(path string) (*DB, error)`
- `OpenWithOptions(path string, OpenOptions) (*DB, error)`：可通过 `MaxConcurrentQueries` 设置同一 DB 的最大并发查询数
- `(*DB).Close() error`
- `DB.Format DatabaseFormat`：当前打开文件的格式信息，包括 `Name/Engine/Version/PageSize/ObjectStorage`
- `(*DB).Tables() ([]string, error)`
- `(*DB).Views() ([]string, error)`：列出 Access 保存查询，与原版 `mdb-queries` 一样包含 `~sq_` 内部查询
- `(*DB).ViewSQL(viewName string) (string, error)`：从 `MSysQueries` 还原 SELECT View SQL，支持参数、别名、INNER/LEFT/RIGHT JOIN、WHERE、GROUP BY、HAVING、多字段排序、外部数据库和 `WITH OWNERACCESS OPTION`
- `(*DB).TableRowCount(tableName string) (uint64, error)`：从表定义页快速读取记录数，不扫描数据行；MDB 异常关闭时该元数据可能为 `0`
- `(*DB).ReadTable(tableName string) (*TableData, error)`
- `(*DB).Query(sql string) (*TableData, error)`：兼容字符串结果 API，由 Go SQL 引擎执行
- `(*DB).QueryContext(ctx, sql, params) (*Rows, error)`：类型化结果、命名参数与取消
- `(*DB).QueryViewContext(ctx, viewName, params) (*Rows, error)`：执行 Access 保存查询
- `(*DB).QueryPageContext(ctx, sql, params, PageRequest) (*PageResult, error)`：带数据库与查询指纹校验的游标分页；单表可推导默认顺序，复杂查询要求显式 `ORDER BY`
- `(*DB).PreparePagerContext(ctx, sql, params, pageSize) (*Pager, error)`：物化到临时 spool，支持稳定的任意页读取
- `(*DB).Schema(tableName string) (*TableSchema, error)`
- `(*DB).Schemas() ([]*TableSchema, error)`：一次目录扫描读取所有用户表 schema；每项同时包含表定义页中的 `RowCount`
- `(*DB).ExportForms() ([]FormInfo, error)`：导出 Access Form 窗体和组件属性
- `(*DB).ExportForm(formName string) (*FormInfo, error)`：按名称导出单个 Access Form 窗体和组件属性（名称不区分大小写）
- `(*DB).ReadAccessObjectContainer() (*AccessObjectContainer, error)`：重组 `MSysAccessObjects.Data` 中的内部 OLE Compound 容器
- `(*DB).ReadAccessObjectEntries() ([]AccessObjectEntry, error)`：读取内部 OLE 的目录与数据流
- `(*DB).ReadFormObjectStreams(formName string) (*FormObjectStreams, error)`：读取指定窗体的 `Blob/TypeInfo/PropData/BlobDelta`
- `(*DB).ExportFormContent(formName string) (*FormContent, error)`：只读取并完整导出指定窗体，不遍历其他窗体
- `(*DB).ReadFormContent(formName string) (*FormContent, error)`：读取单个窗体的 Caption、RecordSource、窗体宽度、`FormHeader/Detail/FormFooter` 分区、控件名、准确类型、twips 几何和常用文本属性；TextBox 额外直接输出 `Format/Tag/FontName/StatusBarText/TabIndex`，Label 直接输出 `Caption/Tag/FontName/FontSize/TextAlign`
- `(*DB).ExportFormContents() ([]FormContent, error)`：一次重组 OLE 后批量导出全部窗体内容
- `ParseFormTypeInfo(data []byte) ([]FormControlInfo, error)`：解析控件名、内部类型代码和索引
- `ParseFormContent(streams *FormObjectStreams) (*FormContent, error)`：解析已读取的窗体设计流
- `(*DB).ReadFormStreams(formName string) (*FormStreams, error)`：读取窗体原始设计流（Lv/LvProp/LvExtra）
- `(*DB).ReadFormAccessObjectChunks(formName string) ([]AccessObjectChunk, error)`：读取窗体相关的 AccessObjects 分片（按相关度排序）
- `(*DB).ReadFormDesignChunks(formName string) ([]FormDesignChunk, error)`：筛选窗体设计强相关分片（DocClass/VB_Name/NameMap 命中）
- `(*DB).ReadAndParseFormLayout(formName string) (*FormLayout, error)`：优先用 TypeInfo 返回准确控件目录，旧分片解析作为兜底
- `ParseFormPropsFromLvProp(lvProp []byte) (*ParsedFormProps, error)`：结构化解析 LvProp（含 NameMap 控件名）
- `ParseFormLayoutFromDesignChunks(formName string, chunks []FormDesignChunk, parsed *ParsedFormProps) *FormLayout`：分片级布局解析
- `ParseFormLayoutFromStreams(streams *FormStreams) *FormLayout`：用 Go 对二进制流做启发式解析
- `ControlTypeCodeToString(ctype int) string`：把 Access 控件类型代码转换为类型名

## SQL 支持范围

当前 Go 引擎支持 `SELECT`、`TOP/PERCENT`、`DISTINCT/DISTINCTROW`、字段与表别名、
`INNER/LEFT/RIGHT/CROSS JOIN`、`WHERE`、`GROUP BY`、`HAVING`、`ORDER BY`、
`LIMIT/OFFSET`、`UNION/UNION ALL`、标量/`IN`/`EXISTS` 子查询及保存查询递归执行。
常用表达式包括 Access 三值 NULL 逻辑、`LIKE` 通配符、`BETWEEN`、`IN`、日期字面量、
字符串连接，以及 `IIf/Nz/DatePart/Format/Left/Right/Mid/Len` 等函数。

查询使用 1024 行批次跨越 CGO，并只绑定引用列；简单 `TOP/LIMIT` 会提前停止扫描，
等值连接使用 Hash Join。`INSERT/UPDATE/DELETE/DDL`、Pass-through、VBA 自定义函数、
外部数据库 `IN` 和 `TRANSFORM/PIVOT` 尚不支持，遇到时返回明确错误。

## 并发查询

`Query`、`QueryContext`、`QueryViewContext`、`QueryPageContext` 和
`PreparePagerContext` 可以在同一个 `*DB` 上由多个 goroutine 并发调用。实现会为每个
正在运行的查询租用独立的 mdbtools 句柄，并在查询完成后放回池中复用，避免共享
`MdbHandle` 页缓冲区产生数据竞争。

默认并发数取 `GOMAXPROCS`，最少 2、最多 8；显式配置上限为 64。可按数据库大小和
可用内存限制：

```go
db, err := mdbgo.OpenWithOptions("example.mdb", mdbgo.OpenOptions{
    MaxConcurrentQueries: 4,
})
```

超过并发上限的查询会等待空闲句柄，等待过程响应 `context.Context` 取消。`Close`
会拒绝新查询，并等待已经开始的查询完成后释放全部池化句柄。`Tables`、
`Views`、`Schema`、`ReadTable` 和 Form 读取接口仍使用主句柄，不应与彼此或 `Close`
并发调用。

## 测试导出 Access 原生结构 JSON

`TestExportFormAsAccessJSON` 可以指定任意窗体，按 `testdb/t_abia_master_org.json` 的字段命名和 `TabControl/TabPage/SubForm` 层级输出未做像素或颜色转换的原生数值：

```bash
MDBGO_EXPORT_FORM_NAME=f_abia_master \
go test -run TestExportFormAsAccessJSON -v -count=1

MDBGO_TEST_DB=testdb/mdbs/eIT.mdb \
MDBGO_EXPORT_FORM_NAME=f_act_branch_query \
MDBGO_EXPORT_FORM_OUTPUT=testdb/f_act_branch_query_mdbgo.json \
go test -run TestExportFormAsAccessJSON -v -count=1
```

可同时使用 `MDBGO_TEST_DB=/path/to/database.mdb` 指定其它 MDB 文件。未设置 `MDBGO_EXPORT_FORM_OUTPUT` 时 JSON 输出到测试日志。Windows COM `GetHashCode()` 是运行时值，并不存在于 MDB 持久数据中，因此该测试不会输出 `Hash`。

## 说明

- 为了简化跨平台构建，bundled 模式默认关闭 `iconv`。
- 当前 `DB` 句柄按串行访问设计，不保证并发安全。
- `Query` 不允许 `CONNECT` / `DISCONNECT` 语句。
- Jet4 MDB 已支持 `RecordSource`、窗体 `Width`，以及控件 `Name/Type/Left/Top/Width/Height/Caption/ControlSource/Format/Tag/FontName/FontSize/StatusBarText/TextAlign/TabIndex` 等常用内容。
- `TextAlignValue/BackColorValue/ForeColorValue` 保留 Access Interop 原生数值；`TextAlign/BackColor/ForeColor/BackGroundColor` 提供便于直接使用的文本值。`f_abia_master` 的 36 个 TextBox 和 35 个原生 Label 已分别通过 Windows 导出 JSON 的字段级对照。
- `FormContent.Sections` 按 TypeInfo 中的分区标记组织真实控件；`FormContent.Controls` 仍保留包含分区标记的原始平面顺序，普通控件可通过 `Section` 判断归属。
- `FormPropertyIDToName` 的常用属性编号按 Microsoft Office Access Interop 的 `DispId` 对齐；例如 `ControlSource=27`、`Format=38`、`InputMask=72`、`Tag=266`。
- `HasGeometry=true` 表示控件块包含完整的四项 twips 几何标签；节、继承布局或省略默认值的少量控件会保持 `HasGeometry=false`，不会用猜测值填充。
