package yasdb

import (
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// VectorDistanceMetric 向量索引距离度量。
type VectorDistanceMetric string

const (
	VectorDistanceCosine           VectorDistanceMetric = "COSINE"
	VectorDistanceEuclidean        VectorDistanceMetric = "EUCLIDEAN"
	VectorDistanceDot              VectorDistanceMetric = "DOT"
	VectorDistanceEuclideanSquared VectorDistanceMetric = "EUCLIDEAN_SQUARED"
	VectorDistanceL2Squared        VectorDistanceMetric = "L2_SQUARED"
)

// VectorHNSWParams HNSW 向量索引构建参数。
type VectorHNSWParams struct {
	M              int
	EFConstruction int
}

// VectorAlterIndexOptions 修改向量索引的参数（对齐 ALTER INDEX 语句）。
type VectorAlterIndexOptions struct {
	IndexName string
	Rebuild   bool
	// Visible 为 nil 时不修改可见性；true=VISIBLE，false=INVISIBLE。
	Visible *bool
}

// VectorIndexOptions 创建向量索引的参数。
type VectorIndexOptions struct {
	IndexName  string
	TableName  string
	ColumnName string
	Distance   VectorDistanceMetric
	HNSW       *VectorHNSWParams
}

func normalizeVectorDistanceMetric(metric VectorDistanceMetric) (VectorDistanceMetric, error) {
	switch strings.ToUpper(strings.TrimSpace(string(metric))) {
	case "", "COSINE":
		return VectorDistanceCosine, nil
	case "EUCLIDEAN", "L2":
		return VectorDistanceEuclidean, nil
	case "DOT", "IP":
		return VectorDistanceDot, nil
	case "EUCLIDEAN_SQUARED":
		return VectorDistanceEuclideanSquared, nil
	case "L2_SQUARED":
		return VectorDistanceL2Squared, nil
	default:
		return "", fmt.Errorf("unsupported vector distance metric: %s", metric)
	}
}

func normalizeVectorIndexOptions(opts VectorIndexOptions) (VectorIndexOptions, error) {
	opts.IndexName = strings.TrimSpace(opts.IndexName)
	opts.TableName = strings.TrimSpace(opts.TableName)
	opts.ColumnName = strings.TrimSpace(opts.ColumnName)
	if opts.IndexName == "" || opts.TableName == "" || opts.ColumnName == "" {
		return VectorIndexOptions{}, fmt.Errorf("index name, table name and column name are required")
	}
	distance, err := normalizeVectorDistanceMetric(opts.Distance)
	if err != nil {
		return VectorIndexOptions{}, err
	}
	opts.Distance = distance
	opts.IndexName = ConvertNameToFormat(opts.IndexName)
	opts.TableName = ConvertNameToFormat(opts.TableName)
	opts.ColumnName = ConvertNameToFormat(opts.ColumnName)
	return opts, nil
}

func buildVectorIndexHNSWClause(params *VectorHNSWParams) (string, error) {
	if params == nil {
		return "", nil
	}
	parts := make([]string, 0, 3)
	if params.M > 0 {
		if params.M < 2 || params.M > 100 {
			return "", fmt.Errorf("HNSW M must be in [2, 100]")
		}
		parts = append(parts, fmt.Sprintf("M %d", params.M))
	}
	if params.EFConstruction > 0 {
		if params.EFConstruction < 4 || params.EFConstruction > 1000 {
			return "", fmt.Errorf("HNSW EFConstruction must be in [4, 1000]")
		}
		m := params.M
		if m <= 0 {
			m = 16
		}
		if params.EFConstruction < m*2 {
			return "", fmt.Errorf("HNSW EFConstruction must be >= 2 * M")
		}
		parts = append(parts, fmt.Sprintf("EFCONSTRUCTION %d", params.EFConstruction))
	}
	if len(parts) == 0 {
		return "", nil
	}
	return " PARAMETERS(TYPE HNSW, " + strings.Join(parts, ", ") + ")", nil
}

func normalizeVectorAlterIndexOptions(opts VectorAlterIndexOptions) (VectorAlterIndexOptions, error) {
	opts.IndexName = ConvertNameToFormat(strings.TrimSpace(opts.IndexName))
	if opts.IndexName == "" {
		return VectorAlterIndexOptions{}, fmt.Errorf("index name is required")
	}
	if !opts.Rebuild && opts.Visible == nil {
		return VectorAlterIndexOptions{}, fmt.Errorf("at least one alter action is required")
	}
	return opts, nil
}

// BuildAlterVectorIndexSQL 生成 ALTER INDEX DDL（REBUILD / VISIBLE / INVISIBLE）。
func BuildAlterVectorIndexSQL(opts VectorAlterIndexOptions) (string, error) {
	normalized, err := normalizeVectorAlterIndexOptions(opts)
	if err != nil {
		return "", err
	}
	parts := []string{"ALTER INDEX", normalized.IndexName}
	if opts.Rebuild {
		parts = append(parts, "REBUILD")
	}
	if opts.Visible != nil {
		if *opts.Visible {
			parts = append(parts, "VISIBLE")
		} else {
			parts = append(parts, "INVISIBLE")
		}
	}
	return strings.Join(parts, " "), nil
}

// AlterVectorIndex 在 Migrator 上修改向量索引。
func (m Migrator) AlterVectorIndex(opts VectorAlterIndexOptions) error {
	sql, err := BuildAlterVectorIndexSQL(opts)
	if err != nil {
		return err
	}
	return m.DB.Exec(sql).Error
}

// BuildCreateVectorIndexSQL 生成 CREATE VECTOR INDEX DDL。
func BuildCreateVectorIndexSQL(opts VectorIndexOptions) (string, error) {
	normalized, err := normalizeVectorIndexOptions(opts)
	if err != nil {
		return "", err
	}
	hnswClause, err := buildVectorIndexHNSWClause(opts.HNSW)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"CREATE VECTOR INDEX %s ON %s (%s) ORGANIZATION NEIGHBOR GRAPH WITH DISTANCE %s%s",
		normalized.IndexName,
		normalized.TableName,
		normalized.ColumnName,
		normalized.Distance,
		hnswClause,
	), nil
}

// CreateVectorIndex 在 Migrator 上创建向量索引。
func (m Migrator) CreateVectorIndex(opts VectorIndexOptions) error {
	sql, err := BuildCreateVectorIndexSQL(opts)
	if err != nil {
		return err
	}
	return m.DB.Exec(sql).Error
}

// HasVectorIndex 判断向量索引是否存在。
func (m Migrator) HasVectorIndex(indexName string) bool {
	if m.DB == nil {
		return false
	}
	var count int64
	name := ConvertNameToFormat(strings.TrimSpace(indexName))
	if name == "" {
		return false
	}
	row := m.DB.Raw("SELECT COUNT(1) FROM USER_INDEXES WHERE INDEX_NAME = ?", name).Row()
	if row == nil {
		return false
	}
	if err := row.Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// DropVectorIndex 删除向量索引。
func (m Migrator) DropVectorIndex(indexName string) error {
	if m.DB == nil {
		return fmt.Errorf("db is nil")
	}
	name := ConvertNameToFormat(strings.TrimSpace(indexName))
	if name == "" {
		return fmt.Errorf("index name is required")
	}
	return m.DB.Exec(fmt.Sprintf("DROP INDEX %s", name)).Error
}

// CreateVectorIndexFromModel 根据模型与字段名创建向量索引。
func CreateVectorIndexFromModel(db *gorm.DB, model interface{}, indexName, fieldName string, distance VectorDistanceMetric) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return err
	}
	field := stmt.Schema.LookUpField(fieldName)
	if field == nil {
		return fmt.Errorf("field %q not found in schema", fieldName)
	}
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  indexName,
		TableName:  stmt.Schema.Table,
		ColumnName: field.DBName,
		Distance:   distance,
	})
	if err != nil {
		return err
	}
	return db.Exec(sql).Error
}
