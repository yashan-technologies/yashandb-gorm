package yasdb

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yashan-technologies/yashandb-gorm/clauses"

	_ "github.com/yashan-technologies/yashandb-go"
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
	DriverName          string
	DSN                 string
	Conn                *sql.DB
	DefaultStringSize   uint
	NamingCaseSensitive bool
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
	if !d.NamingCaseSensitive {
		if namingStrategy, ok := db.NamingStrategy.(schema.NamingStrategy); ok {
		db.NamingStrategy = Namer{NamingStrategy: namingStrategy}
	}
	}

	d.DefaultStringSize = 1024

	// register callbacks
	config := &callbacks.Config{
		CreateClauses: []string{"INSERT", "VALUES", "ON CONFLICT", "RETURNING"},
	}
	callbacks.RegisterDefaultCallbacks(db, config)

	d.DriverName = "yasdb"

	if d.Conn != nil {
		db.ConnPool = d.Conn
	} else {
		db.ConnPool, err = sql.Open(d.DriverName, d.DSN)
		if err != nil {
			return
		}
	}

	for k, v := range d.ClauseBuilders() {
		db.ClauseBuilders[k] = v
	}

	if err = db.Callback().Create().Replace("gorm:create", Create); err != nil {
		return
	}

	registerNormalizeSchemaCallbacks(db)
	registerColumnMappingCallbacks(db)

	return
}

func (d Dialector) ClauseBuilders() map[string]clause.ClauseBuilder {
	return map[string]clause.ClauseBuilder{
		"LIMIT":       d.RewriteLimit,
		"ORDER BY":    d.RewriteOrderBy,
		"WHERE":       d.RewriteWhere,
		"ON CONFLICT": d.RewriteConflict,
	}
}

func resolveOnConflictDoUpdates(onConflict clause.OnConflict, stmt *gorm.Statement) clause.Set {
	if onConflict.DoNothing {
		return nil
	}
	if len(onConflict.DoUpdates) > 0 {
		return onConflict.DoUpdates
	}
	if stmt == nil || stmt.Schema == nil {
		return nil
	}

	collectNonPrimaryColumns := func() []string {
		columns := make([]string, 0)
		for _, field := range stmt.Schema.Fields {
			if field.PrimaryKey {
				continue
			}
			if field.AutoCreateTime > 0 && field.AutoUpdateTime == 0 {
				continue
			}
			columns = append(columns, field.DBName)
		}
		return columns
	}

	if onConflict.UpdateAll || len(onConflict.DoUpdates) == 0 {
		if columns := collectNonPrimaryColumns(); len(columns) > 0 {
			return clause.AssignmentColumns(columns)
		}
	}
	return nil
}

func writeConflictAssignment(builder clause.Builder, assignment clause.Assignment) {
	col := assignment.Column
	col.Table = ""
	builder.WriteQuoted(col)
	_ = builder.WriteByte('=')

	if column, ok := assignment.Value.(clause.Column); ok && column.Table == "excluded" {
		_, _ = builder.WriteString("VALUES(")
		builder.WriteQuoted(clause.Column{Name: column.Name})
		_ = builder.WriteByte(')')
		return
	}
	if column, ok := assignment.Value.(clause.Column); ok {
		column.Table = ""
		builder.WriteQuoted(column)
		return
	}
	builder.AddVar(builder, assignment.Value)
}

func (d Dialector) RewriteConflict(c clause.Clause, builder clause.Builder) {
	if onConflict, ok := c.Expression.(clause.OnConflict); ok {
		stmt, ok := builder.(*gorm.Statement)
		if !ok {
			_, _ = builder.WriteString("ON DUPLICATE KEY UPDATE ")
			return
		}
		doUpdates := resolveOnConflictDoUpdates(onConflict, stmt)

		_, _ = builder.WriteString("ON DUPLICATE KEY UPDATE ")
		if len(doUpdates) == 0 {
			// 无字段需要更新时，用主键自赋值实现空操作，避免无意义的 VALUES(主键) 更新
			if stmt != nil && stmt.Schema != nil && stmt.Schema.PrioritizedPrimaryField != nil {
				col := clause.Column{Name: stmt.Schema.PrioritizedPrimaryField.DBName}
				builder.WriteQuoted(col)
				_ = builder.WriteByte('=')
				builder.WriteQuoted(col)
			}
			return
		}

		for idx, assignment := range doUpdates {
			if idx > 0 {
				_ = builder.WriteByte(',')
			}
			writeConflictAssignment(builder, assignment)
		}
	}
}

func (d Dialector) RewriteWhere(c clause.Clause, builder clause.Builder) {
	if where, ok := c.Expression.(clause.Where); ok {
		_, _ = builder.WriteString(" WHERE ")

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
					_, _ = builder.WriteString(" OR ")
				} else {
					_, _ = builder.WriteString(" AND ")
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
				_, _ = builder.WriteString(`(`)
				expr.Build(builder)
				_, _ = builder.WriteString(`)`)
				wrapInParentheses = false
			} else {
				if e, ok := expr.(clause.IN); ok {
					if len(e.Values) < 1 {
						expr.Build(builder)
						continue
					}
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

func (d Dialector) RewriteOrderBy(c clause.Clause, builder clause.Builder) {
	if orderBy, ok := c.Expression.(clause.OrderBy); ok {
		_, _ = builder.WriteString("ORDER BY ")
		orderBy.Build(builder)
	}
}

func (d Dialector) RewriteLimit(c clause.Clause, builder clause.Builder) {
	if limit, ok := c.Expression.(clause.Limit); ok {
		if stmt, ok := builder.(*gorm.Statement); ok {
			if vs, ok := vectorSearchFromStatement(stmt); ok {
				if limit.Limit != nil && *limit.Limit > 0 {
					if vs.Approximate {
						_, _ = builder.WriteString(" FETCH APPROXIMATE FIRST ")
					} else {
						_, _ = builder.WriteString(" FETCH FIRST ")
					}
					_, _ = builder.WriteString(strconv.Itoa(*limit.Limit))
					_, _ = builder.WriteString(" ROWS ONLY")
				}
				if offset := limit.Offset; offset > 0 {
					_, _ = builder.WriteString(" OFFSET ")
					_, _ = builder.WriteString(strconv.Itoa(offset))
				}
				return
			}
			if _, ok := stmt.Clauses["ORDER BY"]; !ok {
				s := stmt.Schema
				_, _ = builder.WriteString("ORDER BY ")
				if s != nil && s.PrioritizedPrimaryField != nil {
					builder.WriteQuoted(s.PrioritizedPrimaryField.DBName)
					_ = builder.WriteByte(' ')
				} else {
					_, _ = builder.WriteString("(SELECT NULL FROM ")
					_, _ = builder.WriteString(d.DummyTableName())
					_, _ = builder.WriteString(")")
				}
			}
		}
		if limit.Limit != nil && *limit.Limit > 0 {
			_, _ = builder.WriteString(" LIMIT ")
			_, _ = builder.WriteString(strconv.Itoa(*limit.Limit))
		}
		if offset := limit.Offset; offset > 0 {
			_, _ = builder.WriteString(" OFFSET ")
			_, _ = builder.WriteString(strconv.Itoa(offset))
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
	_, _ = writer.WriteString(":")
	_, _ = writer.WriteString(strconv.Itoa(len(stmt.Vars)))
}

func (d Dialector) QuoteTo(writer clause.Writer, str string) {
	if len(str) >= 2 && str[0] == '"' && str[len(str)-1] == '"' {
		_, _ = writer.WriteString(str)
		return
	}
	if d.NamingCaseSensitive || IsReservedWord(str) {
		if IsReservedWord(str) && !d.NamingCaseSensitive {
			_, _ = writer.WriteString(fmt.Sprintf(`"%s"`, strings.ToUpper(str)))
		} else {
			_, _ = writer.WriteString(fmt.Sprintf(`"%s"`, str))
		}
		return
	}
	_, _ = writer.WriteString(ConvertNameToFormat(str))
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
		sqlType = "BLOB"
	case schema.Bytes:
		sqlType = "BLOB"
	default:
		sqlType = string(field.DataType)

		if strings.EqualFold(sqlType, "text") {
			sqlType = "CLOB"
		}
		if normalized, ok := NormalizeVectorSQLType(sqlType); ok {
			sqlType = normalized
		} else if looksLikeVectorDataType(sqlType) {
			panic(fmt.Sprintf("unsupported vector format in gorm type %q for yasdb (supported: FLOAT32, FLOAT64)", sqlType))
		} else if sqlType == "" {
			panic(fmt.Sprintf("invalid sql type %s (%s) for yasdb", field.FieldType.Name(), field.FieldType.String()))
		} else {
			sqlType += addStringDefault()
		}
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
