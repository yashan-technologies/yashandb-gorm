package yasdb

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// VectorReturningType FROM_VECTOR / VECTOR_SERIALIZE 的 RETURNING 类型。
type VectorReturningType string

const (
	VectorReturningVarchar VectorReturningType = "VARCHAR"
	VectorReturningClob    VectorReturningType = "CLOB"
)

// ToVectorOptions TO_VECTOR 构造参数。
type ToVectorOptions struct {
	Dimension int
	Format    string // FLOAT32 或 FLOAT64，省略为 FLOAT32
}

// FromVectorOptions FROM_VECTOR / VECTOR_SERIALIZE 参数。
type FromVectorOptions struct {
	FieldName  string
	ColumnName string
	Returning  VectorReturningType
	Size       int // RETURNING VARCHAR 时的长度，(0, 65534]
}

// BuildToVectorSQL 生成 TO_VECTOR(expr[, dim[, format]]) 表达式。
func BuildToVectorSQL(valueExpr string, opts ToVectorOptions) (string, error) {
	valueExpr = strings.TrimSpace(valueExpr)
	if valueExpr == "" {
		return "", fmt.Errorf("value expression is required")
	}
	if opts.Dimension <= 0 {
		return "", fmt.Errorf("dimension must be positive")
	}
	format := strings.TrimSpace(opts.Format)
	if format == "" {
		format = "FLOAT32"
	} else {
		normalized, ok := normalizeVectorFormat(format)
		if !ok {
			return "", fmt.Errorf("unsupported vector format: %s", format)
		}
		format = normalized
	}
	return fmt.Sprintf("TO_VECTOR(%s, %d, %s)", valueExpr, opts.Dimension, format), nil
}

// BuildFromVectorSQL 生成 FROM_VECTOR(column [RETURNING ...]) 表达式。
func BuildFromVectorSQL(columnExpr string, opts FromVectorOptions) (string, error) {
	columnExpr = strings.TrimSpace(columnExpr)
	if columnExpr == "" {
		return "", fmt.Errorf("column expression is required")
	}
	returning := strings.ToUpper(strings.TrimSpace(string(opts.Returning)))
	switch returning {
	case "", string(VectorReturningVarchar):
		if opts.Size <= 0 || opts.Size > 65534 {
			return "", fmt.Errorf("VARCHAR returning size must be in (0, 65534]")
		}
		return fmt.Sprintf("FROM_VECTOR(%s RETURNING VARCHAR(%d))", columnExpr, opts.Size), nil
	case string(VectorReturningClob):
		return fmt.Sprintf("FROM_VECTOR(%s RETURNING CLOB)", columnExpr), nil
	default:
		return "", fmt.Errorf("unsupported returning type: %s", opts.Returning)
	}
}

// BuildVectorNormSQL 生成 VECTOR_NORM(column) 表达式。
func BuildVectorNormSQL(columnExpr string) (string, error) {
	columnExpr = strings.TrimSpace(columnExpr)
	if columnExpr == "" {
		return "", fmt.Errorf("column expression is required")
	}
	return fmt.Sprintf("VECTOR_NORM(%s)", columnExpr), nil
}

// BuildVectorDimensionCountSQL 生成 VECTOR_DIMENSION_COUNT(column) 表达式。
func BuildVectorDimensionCountSQL(columnExpr string) (string, error) {
	columnExpr = strings.TrimSpace(columnExpr)
	if columnExpr == "" {
		return "", fmt.Errorf("column expression is required")
	}
	return fmt.Sprintf("VECTOR_DIMENSION_COUNT(%s)", columnExpr), nil
}

// BuildVectorDimensionFormatSQL 生成 VECTOR_DIMENSION_FORMAT(column) 表达式。
func BuildVectorDimensionFormatSQL(columnExpr string) (string, error) {
	columnExpr = strings.TrimSpace(columnExpr)
	if columnExpr == "" {
		return "", fmt.Errorf("column expression is required")
	}
	return fmt.Sprintf("VECTOR_DIMENSION_FORMAT(%s)", columnExpr), nil
}

func resolveVectorColumnRef(stmt *gorm.Statement, fieldName, columnName string) (string, error) {
	columnName = strings.TrimSpace(columnName)
	if columnName == "" {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			return "", fmt.Errorf("field name or column name is required")
		}
		if stmt == nil || stmt.Schema == nil {
			return "", fmt.Errorf("schema is required to resolve field %q", fieldName)
		}
		field := stmt.Schema.LookUpField(fieldName)
		if field == nil {
			return "", fmt.Errorf("field %q not found in schema", fieldName)
		}
		columnName = field.DBName
	}
	col := ConvertNameToFormat(columnName)
	if stmt != nil && stmt.Table != "" {
		col = ConvertNameToFormat(stmt.Table) + "." + col
	}
	return col, nil
}

func parseModelStmt(db *gorm.DB, model interface{}) (*gorm.Statement, error) {
	if db == nil {
		return nil, fmt.Errorf("db is nil")
	}
	if model == nil {
		return nil, fmt.Errorf("model is required")
	}
	stmt := db.Session(&gorm.Session{}).Model(model).Statement
	if err := stmt.Parse(model); err != nil {
		return nil, err
	}
	return stmt, nil
}

// VectorNorm 查询向量列的 L2 范数。
func VectorNorm(db *gorm.DB, model interface{}, fieldName string) (float64, error) {
	stmt, err := parseModelStmt(db, model)
	if err != nil {
		return 0, err
	}
	col, err := resolveVectorColumnRef(stmt, fieldName, "")
	if err != nil {
		return 0, err
	}
	expr, err := BuildVectorNormSQL(col)
	if err != nil {
		return 0, err
	}
	var norm float64
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE ROWNUM = 1", expr, ConvertNameToFormat(stmt.Table))).Scan(&norm).Error; err != nil {
		return 0, err
	}
	return norm, nil
}

// VectorDimensionCount 查询向量列维度。
func VectorDimensionCount(db *gorm.DB, model interface{}, fieldName string) (int64, error) {
	stmt, err := parseModelStmt(db, model)
	if err != nil {
		return 0, err
	}
	col, err := resolveVectorColumnRef(stmt, fieldName, "")
	if err != nil {
		return 0, err
	}
	expr, err := BuildVectorDimensionCountSQL(col)
	if err != nil {
		return 0, err
	}
	var dim int64
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE ROWNUM = 1", expr, ConvertNameToFormat(stmt.Table))).Scan(&dim).Error; err != nil {
		return 0, err
	}
	return dim, nil
}

// VectorDimensionFormat 查询向量列元素格式（FLOAT32/FLOAT64）。
func VectorDimensionFormat(db *gorm.DB, model interface{}, fieldName string) (string, error) {
	stmt, err := parseModelStmt(db, model)
	if err != nil {
		return "", err
	}
	col, err := resolveVectorColumnRef(stmt, fieldName, "")
	if err != nil {
		return "", err
	}
	expr, err := BuildVectorDimensionFormatSQL(col)
	if err != nil {
		return "", err
	}
	var format string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE ROWNUM = 1", expr, ConvertNameToFormat(stmt.Table))).Scan(&format).Error; err != nil {
		return "", err
	}
	return format, nil
}

// FromVectorString 将向量列序列化为字符串（默认 RETURNING VARCHAR）。
func FromVectorString(db *gorm.DB, model interface{}, opts FromVectorOptions) (string, error) {
	stmt, err := parseModelStmt(db, model)
	if err != nil {
		return "", err
	}
	col, err := resolveVectorColumnRef(stmt, opts.FieldName, opts.ColumnName)
	if err != nil {
		return "", err
	}
	if opts.Returning == "" {
		opts.Returning = VectorReturningVarchar
	}
	if opts.Returning == VectorReturningVarchar && opts.Size <= 0 {
		opts.Size = 4000
	}
	expr, err := BuildFromVectorSQL(col, opts)
	if err != nil {
		return "", err
	}
	var out string
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE ROWNUM = 1", expr, ConvertNameToFormat(stmt.Table))).Scan(&out).Error; err != nil {
		return "", err
	}
	return out, nil
}
