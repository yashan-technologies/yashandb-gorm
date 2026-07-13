package yasdb

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// vectorHighDimSearchModel 用于高维 FLOAT64 检索 SQL 拼装测试。
type vectorHighDimSearchModel struct {
	ID        int64  `gorm:"primaryKey"`
	DocType   string `gorm:"column:doc_type"`
	Embedding Vector `gorm:"column:doc_embedding;type:vector(768,float64);not null"`
}

func (vectorHighDimSearchModel) TableName() string { return "rag_chunk_store" }

func buildVectorSearchSQLDryRun(t *testing.T, model interface{}, setup func(*gorm.DB) *gorm.DB, opts VectorSearchOptions) string {
	t.Helper()
	db := openVectorTestDB(t)
	db = db.Session(&gorm.Session{DryRun: true}).Model(model)
	tx := setup(db)
	tx = ApplyVectorSearch(tx, opts)
	if tx.Error != nil {
		t.Fatalf("apply vector search: %v", tx.Error)
	}
	dest := reflect.New(reflect.TypeOf(model).Elem()).Interface()
	return tx.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return tx.Find(dest)
	})
}

// --- P0：DDL 与类型映射 ---

func TestComposeVectorDDL_ComplexCreateTable(t *testing.T) {
	d := Dialector{Config: &Config{}}
	cols := []struct {
		name string
		tag  string
	}{
		{"title_vec", "vector(128,float32)"},
		{"body_vec", "vector(768,float64)"},
		{"summary_vec", "vector(1536,f32)"},
	}
	parts := make([]string, 0, len(cols)+1)
	parts = append(parts, "id BIGINT PRIMARY KEY")
	for _, c := range cols {
		field := &schema.Field{
			DataType:  schema.DataType(c.tag),
			FieldType: reflect.TypeOf(Vector{}),
		}
		parts = append(parts, fmt.Sprintf("%s %s NOT NULL", strings.ToUpper(c.name), d.DataTypeOf(field)))
	}
	ddl := fmt.Sprintf("CREATE TABLE RAG_DOCUMENTS (\n  %s\n)%s",
		strings.Join(parts, ",\n  "), VectorHeapTableOption())

	for _, want := range []string{
		"TITLE_VEC VECTOR(128, FLOAT32) NOT NULL",
		"BODY_VEC VECTOR(768, FLOAT64) NOT NULL",
		"SUMMARY_VEC VECTOR(1536, FLOAT32) NOT NULL",
		"ORGANIZATION HEAP",
	} {
		if !strings.Contains(ddl, want) {
			t.Fatalf("ddl missing %q:\n%s", want, ddl)
		}
	}
}

func TestComposeVectorDDL_AutoMigrateHeapOption_Complex(t *testing.T) {
	opts := mergeVectorTableOption(" COMPRESS", true)
	ddl := fmt.Sprintf("CREATE TABLE DOC_VECTOR_STORE (ID BIGINT, DOC_EMBEDDING VECTOR(768, FLOAT32))%s", opts)
	if !strings.Contains(ddl, "COMPRESS") || !strings.Contains(ddl, "ORGANIZATION HEAP") {
		t.Fatalf("unexpected ddl: %s", ddl)
	}
}

// --- P1：向量索引 DDL ---

func TestComposeVectorIndexPipeline_ComplexSQL(t *testing.T) {
	tables := []struct {
		table  string
		column string
		metric VectorDistanceMetric
		m      int
		efc    int
	}{
		{"DOC_VECTOR_STORE", "doc_embedding", VectorDistanceCosine, 16, 64},
		{"IMAGE_VECTORS", "feature_emb", VectorDistanceEuclidean, 32, 128},
		{"RECOMMEND_ITEM", "item_vec", VectorDistanceDot, 24, 96},
		{"METRIC_L2SQ", "emb", VectorDistanceEuclideanSquared, 16, 64},
	}
	var stmts []string
	for i, tc := range tables {
		create, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
			IndexName:  fmt.Sprintf("idx_vec_%d", i),
			TableName:  tc.table,
			ColumnName: tc.column,
			Distance:   tc.metric,
			HNSW:       &VectorHNSWParams{M: tc.m, EFConstruction: tc.efc},
		})
		if err != nil {
			t.Fatalf("create index %d: %v", i, err)
		}
		stmts = append(stmts, create)
		invisible := false
		alter, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
			IndexName: fmt.Sprintf("idx_vec_%d", i),
			Visible:   &invisible,
		})
		if err != nil {
			t.Fatalf("alter index %d: %v", i, err)
		}
		stmts = append(stmts, alter)
		stmts = append(stmts, fmt.Sprintf("DROP INDEX %s", ConvertNameToFormat(fmt.Sprintf("idx_vec_%d", i))))
	}
	script := strings.Join(stmts, ";\n") + ";"
	for _, fragment := range []string{
		"CREATE VECTOR INDEX IDX_VEC_0 ON DOC_VECTOR_STORE (DOC_EMBEDDING)",
		"DISTANCE COSINE PARAMETERS(TYPE HNSW, M 16, EFCONSTRUCTION 64)",
		"DISTANCE EUCLIDEAN PARAMETERS(TYPE HNSW, M 32, EFCONSTRUCTION 128)",
		"DISTANCE DOT",
		"DISTANCE EUCLIDEAN_SQUARED",
		"ALTER INDEX IDX_VEC_0 INVISIBLE",
		"DROP INDEX IDX_VEC_3",
	} {
		if !strings.Contains(script, fragment) {
			t.Fatalf("pipeline script missing %q:\n%s", fragment, script)
		}
	}
}

func TestComposeVectorIndexWithCreateTable_Complex(t *testing.T) {
	d := Dialector{Config: &Config{}}
	embField := &schema.Field{
		DataType:  schema.DataType("vector(768,float32)"),
		FieldType: reflect.TypeOf(Vector{}),
	}
	createTable := fmt.Sprintf(`CREATE TABLE KNOWLEDGE_BASE (
  KB_ID BIGINT PRIMARY KEY,
  CHUNK_TEXT CLOB,
  EMBEDDING %s NOT NULL
)%s`, d.DataTypeOf(embField), VectorHeapTableOption())

	createIndex, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "kb_hnsw_idx",
		TableName:  "knowledge_base",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
		HNSW:       &VectorHNSWParams{M: 16, EFConstruction: 64},
	})
	if err != nil {
		t.Fatalf("index: %v", err)
	}
	script := createTable + ";\n" + createIndex + ";"
	if !strings.Contains(script, "VECTOR(768, FLOAT32)") {
		t.Fatalf("missing vector column: %s", script)
	}
	if !strings.Contains(script, "CREATE VECTOR INDEX KB_HNSW_IDX") {
		t.Fatalf("missing index: %s", script)
	}
}

// --- P2：向量检索 SQL ---

func TestBuildVectorSearchSQL_AllMetrics_Complex(t *testing.T) {
	metrics := []VectorDistanceMetric{
		VectorDistanceCosine,
		VectorDistanceEuclidean,
		VectorDistanceDot,
		VectorDistanceEuclideanSquared,
		"L2",
		"IP",
	}
	for _, metric := range metrics {
		t.Run(string(metric)+"_exact_with_score", func(t *testing.T) {
			sql := buildVectorSearchSQLDryRun(t, &vectorSearchModel{}, func(db *gorm.DB) *gorm.DB {
				return db
			}, VectorSearchOptions{
				FieldName:   "Embedding",
				QueryVector: NewVectorFloat32([]float32{0.1, 0.2, 0.3}),
				TopK:        10,
				Distance:    metric,
				WithScore:   true,
			})
			upper := strings.ToUpper(sql)
			if strings.Contains(upper, "SELECT *") {
				t.Fatalf("must not use SELECT *: %s", sql)
			}
			if !strings.Contains(upper, "VECTOR_DISTANCE") {
				t.Fatalf("missing vector_distance: %s", sql)
			}
			if !strings.Contains(upper, "FETCH FIRST 10 ROWS ONLY") {
				t.Fatalf("missing fetch first: %s", sql)
			}
			if strings.Contains(upper, "APPROXIMATE") {
				t.Fatalf("exact search must not use approximate: %s", sql)
			}
		})
	}
}

func TestBuildVectorSearchSQL_ApproximateAllMetrics_Complex(t *testing.T) {
	metrics := []VectorDistanceMetric{
		VectorDistanceCosine,
		VectorDistanceEuclidean,
		VectorDistanceDot,
		VectorDistanceEuclideanSquared,
	}
	for _, metric := range metrics {
		t.Run(string(metric), func(t *testing.T) {
			sql := buildVectorSearchSQLDryRun(t, &vectorSearchModel{}, func(db *gorm.DB) *gorm.DB {
				return db
			}, VectorSearchOptions{
				ColumnName:  "embedding",
				QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
				TopK:        50,
				Distance:    metric,
				Approximate: true,
			})
			if !strings.Contains(strings.ToUpper(sql), "FETCH APPROXIMATE FIRST 50 ROWS ONLY") {
				t.Fatalf("missing approximate fetch: %s", sql)
			}
		})
	}
}

func TestBuildVectorSearchSQL_WithScalarFilter_Complex(t *testing.T) {
	sql := buildVectorSearchSQLDryRun(t, &vectorSearchModel{}, func(db *gorm.DB) *gorm.DB {
		return db.Where("category IN ? AND id > ?", []string{"tech", "science"}, 100)
	}, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{0.5, 0.5, 0}),
		TopK:        20,
		Distance:    VectorDistanceCosine,
		Approximate: true,
		WithScore:   true,
		ScoreAlias:  "similarity",
	})
	upper := strings.ToUpper(sql)
	for _, want := range []string{
		"CATEGORY IN",
		"ID >",
		"VECTOR_DISTANCE",
		"AS SIMILARITY",
		"FETCH APPROXIMATE FIRST 20 ROWS ONLY",
	} {
		if !strings.Contains(upper, want) {
			t.Fatalf("sql missing %q:\n%s", want, sql)
		}
	}
}

func TestBuildVectorSearchSQL_HighDimFloat64_Complex(t *testing.T) {
	query := NewVectorFloat64(make([]float64, 768))
	for i := range query.Data.([]float64) {
		query.Data.([]float64)[i] = float64(i) * 0.001
	}
	sql := buildVectorSearchSQLDryRun(t, &vectorHighDimSearchModel{}, func(db *gorm.DB) *gorm.DB {
		return db.Where("doc_type = ?", "paragraph")
	}, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        5,
		Distance:    VectorDistanceEuclidean,
		Approximate: true,
		WithScore:   true,
	})
	upper := strings.ToUpper(sql)
	for _, want := range []string{
		"RAG_CHUNK_STORE.DOC_EMBEDDING",
		"VECTOR_DISTANCE",
		"EUCLIDEAN",
		"DOC_TYPE =",
		"FETCH APPROXIMATE FIRST 5 ROWS ONLY",
	} {
		if !strings.Contains(upper, want) {
			t.Fatalf("high-dim sql missing %q:\n%s", want, sql)
		}
	}
}

func TestComposeVectorSearchRAGPipeline_Complex(t *testing.T) {
	toQuery, err := BuildToVectorSQL("?", ToVectorOptions{Dimension: 768, Format: "FLOAT64"})
	if err != nil {
		t.Fatalf("to_vector: %v", err)
	}
	col := "RAG_CHUNK_STORE.DOC_EMBEDDING"
	searchSQL := fmt.Sprintf(`SELECT KB_ID, DOC_TYPE,
  VECTOR_DISTANCE(%s, %s, COSINE) AS SIMILARITY
FROM RAG_CHUNK_STORE
WHERE DOC_TYPE = :1
  AND VECTOR_DIMENSION_COUNT(%s) = 768
  AND VECTOR_DIMENSION_FORMAT(%s) = 'FLOAT64'
ORDER BY SIMILARITY
FETCH APPROXIMATE FIRST 100 ROWS ONLY`, col, toQuery, col, col)

	for _, want := range []string{
		"TO_VECTOR(?, 768, FLOAT64)",
		"VECTOR_DIMENSION_COUNT(RAG_CHUNK_STORE.DOC_EMBEDDING) = 768",
		"FETCH APPROXIMATE FIRST 100 ROWS ONLY",
	} {
		if !strings.Contains(searchSQL, want) {
			t.Fatalf("rag pipeline missing %q:\n%s", want, searchSQL)
		}
	}
}

// --- P3：辅助函数与端到端 DDL 组合 ---

func TestComposeVectorEndToEndSetup_Complex(t *testing.T) {
	d := Dialector{Config: &Config{}}
	embField := &schema.Field{
		DataType:  schema.DataType("vector(768,float32)"),
		FieldType: reflect.TypeOf(Vector{}),
	}
	createTable := fmt.Sprintf(`CREATE TABLE DOC_VECTOR_STORE (
  ID BIGINT PRIMARY KEY,
  DOC_TITLE VARCHAR(512),
  EMBEDDING %s NOT NULL
)%s`, d.DataTypeOf(embField), mergeVectorTableOption(" COMPRESS", true))

	createIndex, _ := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName: "doc_emb_idx", TableName: "doc_vector_store", ColumnName: "embedding",
		Distance: VectorDistanceCosine, HNSW: &VectorHNSWParams{M: 16, EFConstruction: 64},
	})
	toInsert, _ := BuildToVectorSQL("?", ToVectorOptions{Dimension: 768, Format: "FLOAT32"})
	insertSQL := fmt.Sprintf("INSERT INTO DOC_VECTOR_STORE (ID, DOC_TITLE, EMBEDDING) VALUES (:1, :2, %s)", toInsert)
	col := "DOC_VECTOR_STORE.EMBEDDING"
	analyticsSQL := fmt.Sprintf(`SELECT ID, %s, %s, %s FROM DOC_VECTOR_STORE WHERE ROWNUM <= 10`,
		mustBuild1(t, BuildVectorNormSQL, col),
		mustBuild1(t, BuildVectorDimensionCountSQL, col),
		mustBuild(t, BuildFromVectorSQL, col, FromVectorOptions{Returning: VectorReturningVarchar, Size: 8192}),
	)

	script := strings.Join([]string{createTable, createIndex, insertSQL, analyticsSQL}, ";\n") + ";"
	for _, want := range []string{
		"ORGANIZATION HEAP", "COMPRESS",
		"CREATE VECTOR INDEX DOC_EMB_IDX",
		"TO_VECTOR(?, 768, FLOAT32)",
		"VECTOR_NORM(DOC_VECTOR_STORE.EMBEDDING)",
		"FROM_VECTOR(DOC_VECTOR_STORE.EMBEDDING RETURNING VARCHAR(8192))",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("e2e script missing %q:\n%s", want, script)
		}
	}
}

func mustBuild[T any](t *testing.T, fn func(string, T) (string, error), col string, opts T) string {
	t.Helper()
	s, err := fn(col, opts)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return s
}

// overload for single-arg builders
func mustBuild1(t *testing.T, fn func(string) (string, error), col string) string {
	t.Helper()
	s, err := fn(col)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return s
}
