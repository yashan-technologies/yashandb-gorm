package yasdb

import (
	"testing"

	"gorm.io/gorm"
)

type vectorSearchHit struct {
	ID        int
	Category  string
	Embedding Vector
	Distance  float64 `gorm:"column:distance"`
}

type vectorEucModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(2,float32);not null"`
}

func (vectorEucModel) TableName() string { return "ut_vector_euc" }

type vectorHNSWModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorHNSWModel) TableName() string { return "ut_vector_hnsw" }

type vectorE2EModel struct {
	ID        int `gorm:"primaryKey"`
	Title     string
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorE2EModel) TableName() string { return "ut_vector_e2e" }

type vectorDotModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorDotModel) TableName() string { return "ut_vector_dot" }

type vectorL2SqModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(2,float32);not null"`
}

func (vectorL2SqModel) TableName() string { return "ut_vector_l2sq" }

func setupVectorSearchTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	if err := db.Exec(`CREATE TABLE ut_vector_search (
		id INT PRIMARY KEY,
		category VARCHAR(32),
		embedding VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	rows := []vectorSearchModel{
		{ID: 1, Category: "a", Embedding: NewVectorFloat32([]float32{1, 0, 0})},
		{ID: 2, Category: "a", Embedding: NewVectorFloat32([]float32{0.9, 0.1, 0})},
		{ID: 3, Category: "b", Embedding: NewVectorFloat32([]float32{0, 1, 0})},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed row %d: %v", row.ID, err)
		}
	}
	if err := CreateVectorIndexFromModel(db, &vectorSearchModel{}, "ut_vec_search_idx", "Embedding", VectorDistanceCosine); err != nil {
		t.Fatalf("create index: %v", err)
	}
}

func TestVectorSearch_ExactOrder(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSearchTable(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	})

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorSearchHit
	err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        3,
		Distance:    VectorDistanceCosine,
		WithScore:   true,
	}).Find(&hits).Error
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("expected 3 hits, got %d", len(hits))
	}
	if hits[0].ID != 1 {
		t.Fatalf("nearest id=%d want 1", hits[0].ID)
	}
	if hits[1].ID != 2 {
		t.Fatalf("second id=%d want 2", hits[1].ID)
	}
	if hits[0].Distance > hits[1].Distance {
		t.Fatalf("distances not ascending: %v then %v", hits[0].Distance, hits[1].Distance)
	}
}

func TestVectorSearch_Approximate(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSearchTable(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	})

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorSearchModel
	err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        2,
		Distance:    VectorDistanceCosine,
		Approximate: true,
	}).Find(&hits).Error
	if err != nil {
		t.Fatalf("approx search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != 1 {
		t.Fatalf("nearest id=%d want 1", hits[0].ID)
	}
}

func TestVectorSearch_WithScalarFilter(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSearchTable(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	})

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorSearchModel
	err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        5,
		Distance:    VectorDistanceCosine,
	}).Where("category = ?", "a").Find(&hits).Error
	if err != nil {
		t.Fatalf("filtered search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits in category a, got %d", len(hits))
	}
	for _, hit := range hits {
		if hit.Category != "a" {
			t.Fatalf("unexpected category %q", hit.Category)
		}
	}
}

func TestVectorSearch_EuclideanMetric(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_euc_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_euc CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_euc_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_euc CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ut_vector_euc (
		id INT PRIMARY KEY,
		embedding VECTOR(2, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.Create(&vectorEucModel{ID: 1, Embedding: NewVectorFloat32([]float32{0, 0})})
	_ = db.Create(&vectorEucModel{ID: 2, Embedding: NewVectorFloat32([]float32{3, 4})})
	if err := CreateVectorIndexFromModel(db, &vectorEucModel{}, "ut_vec_euc_idx", "Embedding", VectorDistanceEuclidean); err != nil {
		t.Fatalf("index: %v", err)
	}

	query := NewVectorFloat32([]float32{0, 0})
	var hits []vectorEucModel
	err := ApplyVectorSearch(db.Model(&vectorEucModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        2,
		Distance:    VectorDistanceEuclidean,
	}).Find(&hits).Error
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].ID != 1 || hits[1].ID != 2 {
		t.Fatalf("unexpected order: %+v", hits)
	}
}

func TestVectorSearch_CreateIndexWithHNSW(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_hnsw_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_hnsw CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_hnsw_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_hnsw CASCADE CONSTRAINTS").Error
	})

	if err := db.AutoMigrate(&vectorHNSWModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{
		IndexName:  "ut_vec_hnsw_idx",
		TableName:  "ut_vector_hnsw",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
		HNSW: &VectorHNSWParams{
			M:              16,
			EFConstruction: 64,
		},
	}); err != nil {
		t.Fatalf("create hnsw index: %v", err)
	}
	if !mig.HasVectorIndex("ut_vec_hnsw_idx") {
		t.Fatal("hnsw index should exist")
	}
}

func TestVectorE2E_MigrateIndexSearch(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_e2e_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_e2e CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_e2e_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_e2e CASCADE CONSTRAINTS").Error
	})

	if err := db.AutoMigrate(&vectorE2EModel{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	rows := []vectorE2EModel{
		{ID: 1, Title: "a", Embedding: NewVectorFloat32([]float32{1, 0, 0})},
		{ID: 2, Title: "b", Embedding: NewVectorFloat32([]float32{0.9, 0.1, 0})},
		{ID: 3, Title: "c", Embedding: NewVectorFloat32([]float32{0, 1, 0})},
	}
	for _, row := range rows {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{
		IndexName:  "ut_vec_e2e_idx",
		TableName:  "ut_vector_e2e",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
		HNSW:       &VectorHNSWParams{M: 16, EFConstruction: 64},
	}); err != nil {
		t.Fatalf("create index: %v", err)
	}

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorE2EModel
	if err := ApplyVectorSearch(db.Model(&vectorE2EModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        2,
		Distance:    VectorDistanceCosine,
		Approximate: true,
	}).Find(&hits).Error; err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 || hits[0].ID != 1 || hits[1].ID != 2 {
		t.Fatalf("unexpected hits: %+v", hits)
	}
}

func TestVectorSearch_ApproximateMatchesExact(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSearchTable(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	})

	query := NewVectorFloat32([]float32{1, 0, 0})
	opts := VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        3,
		Distance:    VectorDistanceCosine,
	}

	var exact []vectorSearchModel
	if err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), opts).Find(&exact).Error; err != nil {
		t.Fatalf("exact: %v", err)
	}
	opts.Approximate = true
	var approx []vectorSearchModel
	if err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), opts).Find(&approx).Error; err != nil {
		t.Fatalf("approx: %v", err)
	}
	if len(exact) != len(approx) {
		t.Fatalf("len exact=%d approx=%d", len(exact), len(approx))
	}
	for i := range exact {
		if exact[i].ID != approx[i].ID {
			t.Fatalf("rank %d: exact id=%d approx id=%d", i, exact[i].ID, approx[i].ID)
		}
	}
}

func TestVectorSearch_WithScoreNearestDistance(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSearchTable(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_search_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_search CASCADE CONSTRAINTS").Error
	})

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorSearchHit
	if err := ApplyVectorSearch(db.Model(&vectorSearchModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        1,
		Distance:    VectorDistanceCosine,
		WithScore:   true,
	}).Find(&hits).Error; err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected 1 hit, got %d", len(hits))
	}
	if hits[0].ID != 1 {
		t.Fatalf("nearest id=%d want 1", hits[0].ID)
	}
	if hits[0].Distance > 1e-6 {
		t.Fatalf("nearest distance=%v want ~0", hits[0].Distance)
	}
}

func TestVectorSearch_DOTMetric(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_dot_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_dot CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_dot_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_dot CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ut_vector_dot (
		id INT PRIMARY KEY,
		embedding VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.Create(&vectorDotModel{ID: 1, Embedding: NewVectorFloat32([]float32{1, 0, 0})})
	_ = db.Create(&vectorDotModel{ID: 2, Embedding: NewVectorFloat32([]float32{0.9, 0.1, 0})})
	_ = db.Create(&vectorDotModel{ID: 3, Embedding: NewVectorFloat32([]float32{0, 1, 0})})
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{
		IndexName:  "ut_vec_dot_idx",
		TableName:  "ut_vector_dot",
		ColumnName: "embedding",
		Distance:   VectorDistanceDot,
	}); err != nil {
		t.Fatalf("index: %v", err)
	}

	query := NewVectorFloat32([]float32{1, 0, 0})
	var hits []vectorDotModel
	if err := ApplyVectorSearch(db.Model(&vectorDotModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        2,
		Distance:    VectorDistanceDot,
	}).Find(&hits).Error; err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 || hits[0].ID != 1 {
		t.Fatalf("unexpected dot hits: %+v", hits)
	}
}

func TestVectorSearch_EuclideanSquaredMetric(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_l2sq_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_l2sq CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_l2sq_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_l2sq CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ut_vector_l2sq (
		id INT PRIMARY KEY,
		embedding VECTOR(2, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	_ = db.Create(&vectorL2SqModel{ID: 1, Embedding: NewVectorFloat32([]float32{0, 0})})
	_ = db.Create(&vectorL2SqModel{ID: 2, Embedding: NewVectorFloat32([]float32{3, 4})})
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{
		IndexName:  "ut_vec_l2sq_idx",
		TableName:  "ut_vector_l2sq",
		ColumnName: "embedding",
		Distance:   VectorDistanceEuclideanSquared,
	}); err != nil {
		t.Fatalf("index: %v", err)
	}

	query := NewVectorFloat32([]float32{0, 0})
	var hits []vectorL2SqModel
	if err := ApplyVectorSearch(db.Model(&vectorL2SqModel{}), VectorSearchOptions{
		FieldName:   "Embedding",
		QueryVector: query,
		TopK:        2,
		Distance:    VectorDistanceEuclideanSquared,
	}).Find(&hits).Error; err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 || hits[0].ID != 1 {
		t.Fatalf("unexpected l2sq hits: %+v", hits)
	}
}
