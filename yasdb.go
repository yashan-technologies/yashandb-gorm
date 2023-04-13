package yasdb

import (
    "database/sql"
    "fmt"
    "regexp"
    "strconv"
    "strings"

    "git.yasdb.com/cod-noah/gorm-yasdb/clauses"

    _ "git.yasdb.com/go/yasdb-go"
    "gorm.io/gorm"
    "gorm.io/gorm/callbacks"
    "gorm.io/gorm/clause"
    "gorm.io/gorm/logger"
    "gorm.io/gorm/migrator"
    "gorm.io/gorm/schema"
)

const (
    JSON schema.DataType = "json"
)

type Config struct {
    DriverName        string
    DSN               string
    Conn              *sql.DB
    DefaultStringSize uint
}

type Dialector struct {
    *Config
}

func Open(dsn string) gorm.Dialector {
    return &Dialector{Config: &Config{DSN: dsn}}
}

func New(config Config) gorm.Dialector {
    return &Dialector{Config: &config}
}

func GenSequenceName(table string) string {
    return strings.ToUpper(fmt.Sprintf("sequence__%s_", TryRemoveQuotes(table)))
}

func (d Dialector) DummyTableName() string {
    return "DUAL"
}

func (d Dialector) Name() string {
    return "yasdb"
}

func (d Dialector) Initialize(db *gorm.DB) (err error) {
    if namingStrategy, ok := db.NamingStrategy.(schema.NamingStrategy); ok {
        db.NamingStrategy = Namer{NamingStrategy: namingStrategy}
    }

    d.DefaultStringSize = 1024

    // register callbacks
    callbacks.RegisterDefaultCallbacks(db, &callbacks.Config{WithReturning: true})

    d.DriverName = "yasdb"

    if d.Conn != nil {
        db.ConnPool = d.Conn
    } else {
        db.ConnPool, _ = sql.Open(d.DriverName, d.DSN)
    }

    for k, v := range d.ClauseBuilders() {
        db.ClauseBuilders[k] = v
    }

    if err = db.Callback().Create().Replace("gorm:create", Create); err != nil {
        return
    }

    return
}

func (d Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
    return map[string]clause.ClauseBuilder{
        "LIMIT":       d.RewriteLimit,
        "WHERE":       d.RewriteWhere,
        "ON CONFLICT": d.RewriteConflict,
    }
}

func (d Dialector) RewriteConflict(c clause.Clause, builder clause.Builder) {
    if onConflict, ok := c.Expression.(clause.OnConflict); ok {
        builder.WriteString("ON DUPLICATE KEY UPDATE ")
        if len(onConflict.DoUpdates) == 0 {
            if s := builder.(*gorm.Statement).Schema; s != nil {
                var column clause.Column
                onConflict.DoNothing = false

                if s.PrioritizedPrimaryField != nil {
                    column = clause.Column{Name: s.PrioritizedPrimaryField.DBName}
                } else if len(s.DBNames) > 0 {
                    column = clause.Column{Name: s.DBNames[0]}
                }

                if column.Name != "" {
                    onConflict.DoUpdates = []clause.Assignment{{Column: column, Value: column}}
                }
            }
        }

        for idx, assignment := range onConflict.DoUpdates {
            if idx > 0 {
                builder.WriteByte(',')
            }

            builder.WriteQuoted(assignment.Column)
            builder.WriteByte('=')
            if column, ok := assignment.Value.(clause.Column); ok && column.Table == "excluded" {
                column.Table = ""
                // column.Name = TryQuoteReservedWord(column.Name)
                builder.WriteString("(")
                builder.WriteQuoted(column)
                builder.WriteByte(')')
            } else {
                builder.AddVar(builder, assignment.Value)
            }
        }
    }

}

func (d Dialector) RewriteWhere(c clause.Clause, builder clause.Builder) {
    if where, ok := c.Expression.(clause.Where); ok {
        builder.WriteString(" WHERE ")

        // Switch position if the first query expression is a single Or condition
        for idx, expr := range where.Exprs {
            if v, ok := expr.(clause.OrConditions); !ok || len(v.Exprs) > 1 {
                if idx != 0 {
                    where.Exprs[0], where.Exprs[idx] = where.Exprs[idx], where.Exprs[0]
                }
                break
            }
        }

        wrapInParentheses := false
        for idx, expr := range where.Exprs {
            if idx > 0 {
                if v, ok := expr.(clause.OrConditions); ok && len(v.Exprs) == 1 {
                    builder.WriteString(" OR ")
                } else {
                    builder.WriteString(" AND ")
                }
            }

            if len(where.Exprs) > 1 {
                switch v := expr.(type) {
                case clause.OrConditions:
                    if len(v.Exprs) == 1 {
                        if e, ok := v.Exprs[0].(clause.Expr); ok {
                            sql := strings.ToLower(e.SQL)
                            wrapInParentheses = strings.Contains(sql, "and") || strings.Contains(sql, "or")
                        }
                    }
                case clause.AndConditions:
                    if len(v.Exprs) == 1 {
                        if e, ok := v.Exprs[0].(clause.Expr); ok {
                            sql := strings.ToLower(e.SQL)
                            wrapInParentheses = strings.Contains(sql, "and") || strings.Contains(sql, "or")
                        }
                    }
                case clause.Expr:
                    sql := strings.ToLower(v.SQL)
                    wrapInParentheses = strings.Contains(sql, "and") || strings.Contains(sql, "or")
                }
            }

            if wrapInParentheses {
                builder.WriteString(`(`)
                expr.Build(builder)
                builder.WriteString(`)`)
                wrapInParentheses = false
            } else {
                if e, ok := expr.(clause.IN); ok {
                    if values, ok := e.Values[0].([]interface{}); ok {
                        if len(values) > 1 {
                            newExpr := clauses.IN{
                                Column: expr.(clause.IN).Column,
                                Values: expr.(clause.IN).Values,
                            }
                            newExpr.Build(builder)
                            continue
                        }
                    }
                }

                expr.Build(builder)
            }
        }
    }
}

func (d Dialector) RewriteLimit(c clause.Clause, builder clause.Builder) {
    if limit, ok := c.Expression.(clause.Limit); ok {
        if stmt, ok := builder.(*gorm.Statement); ok {
            if _, ok := stmt.Clauses["ORDER BY"]; !ok {
                s := stmt.Schema
                builder.WriteString("ORDER BY ")
                if s != nil && s.PrioritizedPrimaryField != nil {
                    builder.WriteQuoted(s.PrioritizedPrimaryField.DBName)
                    builder.WriteByte(' ')
                } else {
                    builder.WriteString("(SELECT NULL FROM ")
                    builder.WriteString(d.DummyTableName())
                    builder.WriteString(")")
                }
            }
        }
        if limit := limit.Limit; limit > 0 {
            builder.WriteString(" LIMIT ")
            builder.WriteString(strconv.Itoa(limit))
        }
        if offset := limit.Offset; offset > 0 {
            builder.WriteString(" OFFSET ")
            builder.WriteString(strconv.Itoa(offset))
        }
    }
}

func (d Dialector) DefaultValueOf(*schema.Field) clause.Expression {
    return clause.Expr{SQL: "VALUES (DEFAULT)"}
}

func (d Dialector) Migrator(db *gorm.DB) gorm.Migrator {
    return Migrator{
        Migrator: migrator.Migrator{
            Config: migrator.Config{
                DB:                          db,
                Dialector:                   d,
                CreateIndexAfterCreateTable: true,
            },
        },
    }
}

func (d Dialector) BindVarTo(writer clause.Writer, stmt *gorm.Statement, v interface{}) {
    writer.WriteString(":")
    writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
    writer.WriteString(str)
}

var numericPlaceholder = regexp.MustCompile(`:(\d+)`)

func (d Dialector) Explain(sql string, vars ...interface{}) string {
    return logger.ExplainSQL(sql, numericPlaceholder, `'`, vars...)
}

func (d Dialector) DataTypeOf(field *schema.Field) string {
    delete(field.TagSettings, "RESTRICT")

    var sqlType string

    addStringDefault := func() string {
        var defaultStr string
        if value, ok := field.TagSettings["DEFAULT"]; ok {
            if value == "''" {
                field.DefaultValue = ""
                field.DefaultValueInterface = nil
            } else {
                field.NotNull = false
            }
        }
        return defaultStr
    }
    switch field.DataType {
    case schema.Int, schema.Uint:
        sqlType = "BIGINT"
        if field.AutoIncrement {
            sqlType += fmt.Sprintf(" default %s.nextval", GenSequenceName(field.Schema.Table))
        }
    case schema.Float:
        sqlType = "DOUBLE"
    case schema.Bool:
        sqlType = "BOOLEAN"
    case schema.String:
        size := field.Size
        if size == 0 {
            size = int(d.DefaultStringSize)
        }
        if size > 8000 {
            field.Size = -1
            sqlType = "CLOB"
        } else {
            sqlType = fmt.Sprintf("VARCHAR(%d)", size)
        }
        sqlType += addStringDefault()
    case schema.Time:
        sqlType = "TIMESTAMP"
        if field.NotNull || field.PrimaryKey {
            sqlType += " NOT NULL"
        }
    case JSON:
        sqlType = "VARCHAR(8000)"
    case schema.Bytes:
        sqlType = "BLOB"
    default:
        sqlType = string(field.DataType)

        if strings.EqualFold(sqlType, "text") {
            sqlType = "CLOB"
        }
        if sqlType == "" {
            panic(fmt.Sprintf("invalid sql type %s (%s) for yasdb", field.FieldType.Name(), field.FieldType.String()))
        }
        sqlType += addStringDefault()
    }
    return sqlType
}

func (d Dialector) SavePoint(tx *gorm.DB, name string) error {
    tx.Exec("SAVEPOINT " + name)
    return tx.Error
}

func (d Dialector) RollbackTo(tx *gorm.DB, name string) error {
    tx.Exec("ROLLBACK TO SAVEPOINT " + name)
    return tx.Error
}
