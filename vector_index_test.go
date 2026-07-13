package yasdb

import (
	"strings"
	"testing"
)

func TestBuildCreateVectorIndexSQL_ManualExample(t *testing.T) {
	// 对齐手册「创建向量索引」示例（HNSW_INDEX on VECTOR_TABLE）
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "hnsw_index",
		TableName:  "vector_table",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
		HNSW: &VectorHNSWParams{
			M:              16,
			EFConstruction: 64,
		},
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	want := "CREATE VECTOR INDEX HNSW_INDEX ON VECTOR_TABLE (EMBEDDING) ORGANIZATION NEIGHBOR GRAPH WITH DISTANCE COSINE PARAMETERS(TYPE HNSW, M 16, EFCONSTRUCTION 64)"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestBuildCreateVectorIndexSQL_HNSW_EFConstructionOnly(t *testing.T) {
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx",
		TableName:  "t",
		ColumnName: "emb",
		HNSW:       &VectorHNSWParams{EFConstruction: 64},
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	if !strings.Contains(sql, "EFCONSTRUCTION 64") {
		t.Fatalf("expected EFCONSTRUCTION in sql: %q", sql)
	}
}

func TestBuildCreateVectorIndexSQL_AllManualDistances(t *testing.T) {
	metrics := []VectorDistanceMetric{
		VectorDistanceCosine,
		VectorDistanceEuclidean,
		VectorDistanceDot,
		VectorDistanceEuclideanSquared,
	}
	for _, m := range metrics {
		sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
			IndexName:  "idx",
			TableName:  "t",
			ColumnName: "c",
			Distance:   m,
		})
		if err != nil {
			t.Fatalf("metric %s: %v", m, err)
		}
		if !strings.Contains(sql, "DISTANCE "+string(m)) {
			t.Fatalf("metric %s: got %q", m, sql)
		}
	}
}

func TestBuildCreateVectorIndexSQL(t *testing.T) {
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx_emb",
		TableName:  "items",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	want := "CREATE VECTOR INDEX IDX_EMB ON ITEMS (EMBEDDING) ORGANIZATION NEIGHBOR GRAPH WITH DISTANCE COSINE"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestBuildCreateVectorIndexSQL_DefaultDistance(t *testing.T) {
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx1",
		TableName:  "t1",
		ColumnName: "emb",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "DISTANCE COSINE") {
		t.Fatalf("expected default COSINE distance: %q", sql)
	}
}

func TestBuildCreateVectorIndexSQL_DistanceAliases(t *testing.T) {
	cases := map[VectorDistanceMetric]string{
		"L2":  "EUCLIDEAN",
		"IP":  "DOT",
		"dot": "DOT",
	}
	for in, wantMetric := range cases {
		sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
			IndexName:  "idx",
			TableName:  "t",
			ColumnName: "c",
			Distance:   in,
		})
		if err != nil {
			t.Fatalf("metric %q: %v", in, err)
		}
		if !strings.Contains(sql, "DISTANCE "+wantMetric) {
			t.Fatalf("metric %q: got %q", in, sql)
		}
	}
}

func TestBuildCreateVectorIndexSQL_Validation(t *testing.T) {
	_, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		TableName:  "t",
		ColumnName: "c",
	})
	if err == nil {
		t.Fatal("expected error for missing index name")
	}
	_, err = BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx",
		TableName:  "t",
		ColumnName: "c",
		Distance:   "INVALID",
	})
	if err == nil {
		t.Fatal("expected error for invalid distance")
	}
}

func TestNormalizeVectorDistanceMetric(t *testing.T) {
	metric, err := normalizeVectorDistanceMetric("")
	if err != nil || metric != VectorDistanceCosine {
		t.Fatalf("default distance: %v err=%v", metric, err)
	}
	metric, err = normalizeVectorDistanceMetric("L2_SQUARED")
	if err != nil || metric != VectorDistanceL2Squared {
		t.Fatalf("L2_SQUARED: %v err=%v", metric, err)
	}
}

func TestMergeVectorTableOption(t *testing.T) {
	if got := mergeVectorTableOption("", true); got != " ORGANIZATION HEAP" {
		t.Fatalf("got %q", got)
	}
	if got := mergeVectorTableOption(" ORGANIZATION HEAP", true); got != " ORGANIZATION HEAP" {
		t.Fatalf("duplicate heap: %q", got)
	}
	if got := mergeVectorTableOption(" COMPRESS", true); got != " COMPRESS ORGANIZATION HEAP" {
		t.Fatalf("append heap: %q", got)
	}
	if got := mergeVectorTableOption("", false); got != "" {
		t.Fatalf("no heap: %q", got)
	}
	if got := mergeVectorTableOption(" ORGANIZATION INDEX", true); got != " ORGANIZATION INDEX" {
		t.Fatalf("existing organization: %q", got)
	}
}

func TestVectorHeapTableOption(t *testing.T) {
	if got := VectorHeapTableOption(); got != " ORGANIZATION HEAP" {
		t.Fatalf("got %q", got)
	}
}

func TestHasVectorIndex_EmptyName(t *testing.T) {
	m := Migrator{}
	if m.HasVectorIndex("") {
		t.Fatal("empty index name should return false")
	}
	if m.HasVectorIndex("   ") {
		t.Fatal("blank index name should return false")
	}
}

func TestDropVectorIndex_Validation(t *testing.T) {
	m := Migrator{}
	if err := m.DropVectorIndex(""); err == nil {
		t.Fatal("expected error for empty index name")
	}
	if err := m.DropVectorIndex("   "); err == nil {
		t.Fatal("expected error for blank index name")
	}
}

func TestNormalizeVectorDistanceMetric_Invalid(t *testing.T) {
	if _, err := normalizeVectorDistanceMetric("NOPE"); err == nil {
		t.Fatal("expected error")
	}
}

func TestCreateVectorIndexFromModel_InvalidField(t *testing.T) {
	db := openVectorTestDB(t)
	err := CreateVectorIndexFromModel(db, &vectorMigrateModel{}, "idx", "NotExist", VectorDistanceCosine)
	if err == nil {
		t.Fatal("expected error for missing field")
	}
}

func TestSchemaHasVectorField(t *testing.T) {
	db := openVectorTestDB(t)
	if err := db.Statement.Parse(&vectorTestModel{}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !SchemaHasVectorField(db.Statement.Schema) {
		t.Fatal("vectorTestModel should have vector field")
	}
	if err := db.Statement.Parse(&documentTestModel{}); err != nil {
		t.Fatalf("parse blob model: %v", err)
	}
	if SchemaHasVectorField(db.Statement.Schema) {
		t.Fatal("document model should not have vector field")
	}
	if SchemaHasVectorField(nil) {
		t.Fatal("nil schema should be false")
	}
}

func TestAlterVectorIndex_Validation(t *testing.T) {
	m := Migrator{}
	if err := m.AlterVectorIndex(VectorAlterIndexOptions{}); err == nil {
		t.Fatal("expected validation error for empty options")
	}
}

func TestCreateVectorIndex_InvalidOptions(t *testing.T) {
	db := openVectorTestDB(t)
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{}); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestBuildCreateVectorIndexSQL_MaxHNSWParams_Complex(t *testing.T) {
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "kb_ann_idx",
		TableName:  "knowledge_chunks",
		ColumnName: "chunk_embedding",
		Distance:   VectorDistanceCosine,
		HNSW:       &VectorHNSWParams{M: 64, EFConstruction: 128},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, want := range []string{
		"CREATE VECTOR INDEX KB_ANN_IDX ON KNOWLEDGE_CHUNKS (CHUNK_EMBEDDING)",
		"DISTANCE COSINE",
		"PARAMETERS(TYPE HNSW, M 64, EFCONSTRUCTION 128)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("sql missing %q: %s", want, sql)
		}
	}
}

func TestComposeVectorIndexLifecycle_Complex(t *testing.T) {
	create, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName: "lifecycle_idx", TableName: "doc_store", ColumnName: "emb",
		Distance: VectorDistanceEuclidean, HNSW: &VectorHNSWParams{M: 16, EFConstruction: 64},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	visible := false
	invisible, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{IndexName: "lifecycle_idx", Visible: &visible})
	if err != nil {
		t.Fatalf("invisible: %v", err)
	}
	rebuild, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{IndexName: "lifecycle_idx", Rebuild: true})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	script := strings.Join([]string{create, invisible, rebuild, "DROP INDEX LIFECYCLE_IDX"}, ";\n") + ";"
	for _, want := range []string{"CREATE VECTOR INDEX LIFECYCLE_IDX", "ALTER INDEX LIFECYCLE_IDX INVISIBLE", "ALTER INDEX LIFECYCLE_IDX REBUILD", "DROP INDEX LIFECYCLE_IDX"} {
		if !strings.Contains(script, want) {
			t.Fatalf("lifecycle script missing %q:\n%s", want, script)
		}
	}
}
