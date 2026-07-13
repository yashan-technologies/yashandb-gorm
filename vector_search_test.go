package yasdb

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

func TestNormalizeVectorSearchOptions_ByColumnName(t *testing.T) {
	vs, err := normalizeVectorSearchOptions(nil, VectorSearchOptions{
		ColumnName:  "embedding",
		QueryVector: NewVectorFloat32([]float32{1, 2, 3}),
		Distance:    VectorDistanceCosine,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vs.Column.Name != "embedding" {
		t.Fatalf("column name: got %q", vs.Column.Name)
	}
	if vs.DistanceMetric != "COSINE" {
		t.Fatalf("distance metric: got %q", vs.DistanceMetric)
	}
}

func TestVectorDistanceSelectColumns_NoSelectStar(t *testing.T) {
	db := openVectorTestDB(t)
	if err := db.Statement.Parse(&vectorSearchModel{}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	vs, err := normalizeVectorSearchOptions(db.Statement, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
		Distance:    VectorDistanceCosine,
		WithScore:   true,
	})
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	db.Statement.Table = "ut_vector_search"
	selectSQL, vars := vectorDistanceSelectColumns(db.Statement, vs)
	if strings.Contains(selectSQL, "*") {
		t.Fatalf("with score should not use SELECT *: %q", selectSQL)
	}
	upper := strings.ToUpper(selectSQL)
	for _, want := range []string{"UT_VECTOR_SEARCH.ID", "UT_VECTOR_SEARCH.EMBEDDING", "VECTOR_DISTANCE", "AS DISTANCE"} {
		if !strings.Contains(upper, want) {
			t.Fatalf("select missing %q: %s", want, selectSQL)
		}
	}
	if len(vars) != 1 {
		t.Fatalf("expected 1 bind var, got %d", len(vars))
	}
}

func TestBuildVectorSearchSQL_WithScoreNoSelectStar(t *testing.T) {
	db := openVectorTestDB(t)
	db = db.Model(&vectorSearchModel{})
	sql, err := BuildVectorSearchSQL(db.Statement, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
		TopK:        3,
		Distance:    VectorDistanceCosine,
		WithScore:   true,
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	if strings.Contains(sql, "SELECT *") {
		t.Fatalf("with score should not generate SELECT *: %s", sql)
	}
	if !strings.Contains(strings.ToUpper(sql), "VECTOR_DISTANCE") {
		t.Fatalf("expected vector_distance in sql: %s", sql)
	}
}

func TestVectorDistanceMetricSQL(t *testing.T) {
	cases := map[VectorDistanceMetric]string{
		VectorDistanceCosine:           "COSINE",
		VectorDistanceEuclidean:        "EUCLIDEAN",
		"L2":                           "EUCLIDEAN",
		VectorDistanceDot:              "DOT",
		VectorDistanceEuclideanSquared: "EUCLIDEAN_SQUARED",
		VectorDistanceL2Squared:        "L2_SQUARED",
	}
	for in, want := range cases {
		got, err := VectorDistanceMetricSQL(in)
		if err != nil {
			t.Fatalf("metric %q: %v", in, err)
		}
		if got != want {
			t.Fatalf("metric %q: got %q want %q", in, got, want)
		}
	}
}

func TestNormalizeVectorSearchOptions_Validation(t *testing.T) {
	db := openVectorTestDB(t)
	_ = db.Statement.Parse(&vectorSearchModel{})
	_, err := normalizeVectorSearchOptions(db.Statement, VectorSearchOptions{})
	if err == nil {
		t.Fatal("expected error for empty options")
	}
	_, err = normalizeVectorSearchOptions(db.Statement, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{1}),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestBuildVectorSearchSQL_Exact(t *testing.T) {
	db := openVectorTestDB(t)
	db = db.Model(&vectorSearchModel{})
	sql, err := BuildVectorSearchSQL(db.Statement, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
		TopK:        5,
		Distance:    VectorDistanceCosine,
		WithScore:   true,
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	upper := strings.ToUpper(sql)
	for _, want := range []string{"VECTOR_DISTANCE", "COSINE", "FETCH FIRST 5 ROWS ONLY"} {
		if !strings.Contains(upper, want) {
			t.Fatalf("sql missing %q: %s", want, sql)
		}
	}
	if strings.Contains(upper, "APPROXIMATE") {
		t.Fatalf("exact search should not contain APPROXIMATE: %s", sql)
	}
}

func TestBuildVectorSearchSQL_Approximate(t *testing.T) {
	db := openVectorTestDB(t)
	db = db.Model(&vectorSearchModel{})
	sql, err := BuildVectorSearchSQL(db.Statement, VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
		TopK:        3,
		Distance:    VectorDistanceCosine,
		Approximate: true,
	})
	if err != nil {
		t.Fatalf("build sql: %v", err)
	}
	upper := strings.ToUpper(sql)
	if !strings.Contains(upper, "FETCH APPROXIMATE FIRST 3 ROWS ONLY") {
		t.Fatalf("expected FETCH APPROXIMATE syntax: %s", sql)
	}
}

func TestBuildCreateVectorIndexSQL_WithHNSW(t *testing.T) {
	sql, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx_hnsw",
		TableName:  "docs",
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
	want := "CREATE VECTOR INDEX IDX_HNSW ON DOCS (EMBEDDING) ORGANIZATION NEIGHBOR GRAPH WITH DISTANCE COSINE PARAMETERS(TYPE HNSW, M 16, EFCONSTRUCTION 64)"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}
}

func TestBuildCreateVectorIndexSQL_HNSWValidation(t *testing.T) {
	_, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx",
		TableName:  "t",
		ColumnName: "c",
		HNSW:       &VectorHNSWParams{M: 1},
	})
	if err == nil {
		t.Fatal("expected error for invalid M")
	}
	_, err = BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "idx",
		TableName:  "t",
		ColumnName: "c",
		HNSW:       &VectorHNSWParams{M: 16, EFConstruction: 20},
	})
	if err == nil {
		t.Fatal("expected error when EFConstruction < 2*M")
	}
}

func TestApplyVectorSearch_InvalidField(t *testing.T) {
	db := openVectorTestDB(t)
	tx := ApplyVectorSearch(db.Model(&vectorSearchModel{}), VectorSearchOptions{
		FieldName:   "Missing",
		QueryVector: NewVectorFloat32([]float32{1}),
	})
	if tx.Error == nil {
		t.Fatal("expected error for missing field")
	}
}

type vectorSearchModel struct {
	ID        int `gorm:"primaryKey"`
	Category  string
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorSearchModel) TableName() string { return "ut_vector_search" }

func TestBuildVectorSearchSQL_OrderByMetricSemantics_Complex(t *testing.T) {
	db := openVectorTestDB(t)
	db = db.Model(&vectorSearchModel{})
	if err := db.Statement.Parse(&vectorSearchModel{}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	metrics := []VectorDistanceMetric{
		VectorDistanceCosine,
		VectorDistanceEuclidean,
		VectorDistanceDot,
		VectorDistanceEuclideanSquared,
	}
	for _, metric := range metrics {
		t.Run(string(metric), func(t *testing.T) {
			vs, err := normalizeVectorSearchOptions(db.Statement, VectorSearchOptions{
				FieldName:   "Embedding",
				QueryVector: NewVectorFloat32([]float32{0.1, 0.2, 0.3}),
				Distance:    metric,
				WithScore:   true,
				ScoreAlias:  "rank_score",
			})
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			orderExpr := vectorDistanceOrderExpr(vs)
			upper := strings.ToUpper(orderExpr.SQL)
			if !strings.Contains(upper, "VECTOR_DISTANCE") || !strings.Contains(upper, vs.DistanceMetric) {
				t.Fatalf("order expr missing metric %s: %s", vs.DistanceMetric, orderExpr.SQL)
			}
			sql, err := BuildVectorSearchSQL(db.Statement, VectorSearchOptions{
				FieldName:   "Embedding",
				QueryVector: NewVectorFloat32([]float32{0.1, 0.2, 0.3}),
				TopK:        15,
				Distance:    metric,
				WithScore:   true,
				ScoreAlias:  "rank_score",
			})
			if err != nil {
				t.Fatalf("build sql: %v", err)
			}
			sqlUpper := strings.ToUpper(sql)
			for _, want := range []string{"VECTOR_DISTANCE", "AS RANK_SCORE", "FETCH FIRST 15 ROWS ONLY"} {
				if !strings.Contains(sqlUpper, want) {
					t.Fatalf("sql missing %q:\n%s", want, sql)
				}
			}
		})
	}
}

func TestBuildVectorSearchSQL_QualifiedColumnAndSubqueryFilter_Complex(t *testing.T) {
	sql := buildVectorSearchSQLDryRun(t, &vectorSearchModel{}, func(db *gorm.DB) *gorm.DB {
		return db.Where("category = ? AND id IN (SELECT id FROM ut_vector_search WHERE category = ?)", "news", "news")
	}, VectorSearchOptions{
		ColumnName:  "ut_vector_search.embedding",
		QueryVector: NewVectorFloat32([]float32{1, 0, 0}),
		TopK:        8,
		Distance:    VectorDistanceCosine,
		Approximate: true,
		WithScore:   true,
	})
	upper := strings.ToUpper(sql)
	for _, want := range []string{
		"UT_VECTOR_SEARCH.EMBEDDING",
		"CATEGORY =",
		"IN (SELECT",
		"FETCH APPROXIMATE FIRST 8 ROWS ONLY",
	} {
		if !strings.Contains(upper, want) {
			t.Fatalf("sql missing %q:\n%s", want, sql)
		}
	}
}
