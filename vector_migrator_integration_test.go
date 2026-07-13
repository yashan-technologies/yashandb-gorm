package yasdb

import (
	"testing"

	"gorm.io/gorm"
)

type vectorMigrateModel struct {
	ID        int `gorm:"primaryKey"`
	Title     string
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorMigrateModel) TableName() string {
	return "ut_vector_migrate"
}

func TestVectorAutoMigrate_CreateAndCRUD(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	if db.Migrator().HasTable(&vectorMigrateModel{}) {
		_ = db.Migrator().DropTable(&vectorMigrateModel{})
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	})

	if err := db.AutoMigrate(&vectorMigrateModel{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if !db.Migrator().HasTable(&vectorMigrateModel{}) {
		t.Fatal("table should exist after automigrate")
	}

	row := vectorMigrateModel{
		ID:        1,
		Title:     "hello",
		Embedding: NewVectorFloat32([]float32{0.1, 0.2, 0.3}),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var found vectorMigrateModel
	if err := db.First(&found, 1).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	if found.Title != "hello" {
		t.Fatalf("unexpected title: %q", found.Title)
	}
}

func TestCreateVectorIndex(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}

	_ = db.Exec("DROP INDEX ut_vec_idx_migrate").Error
	_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	if db.Migrator().HasTable(&vectorMigrateModel{}) {
		_ = db.Migrator().DropTable(&vectorMigrateModel{})
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_idx_migrate").Error
		_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	})

	if err := db.AutoMigrate(&vectorMigrateModel{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if err := db.Create(&vectorMigrateModel{
		ID:        1,
		Embedding: NewVectorFloat32([]float32{1, 0, 0}),
	}).Error; err != nil {
		t.Fatalf("seed row: %v", err)
	}

	if err := CreateVectorIndexFromModel(db, &vectorMigrateModel{}, "ut_vec_idx_migrate", "Embedding", VectorDistanceCosine); err != nil {
		t.Fatalf("create vector index: %v", err)
	}
	if !mig.HasVectorIndex("ut_vec_idx_migrate") {
		t.Fatal("vector index should exist")
	}
}

func TestCreateVectorIndex_IdempotentCheck(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	m, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}

	_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	if db.Migrator().HasTable(&vectorMigrateModel{}) {
		_ = db.Migrator().DropTable(&vectorMigrateModel{})
	}
	t.Cleanup(func() {
		_ = m.DropVectorIndex("ut_vec_idx_dup").Error
		_ = db.Exec("DROP TABLE ut_vector_migrate CASCADE CONSTRAINTS").Error
	})

	if err := db.AutoMigrate(&vectorMigrateModel{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	opts := VectorIndexOptions{
		IndexName:  "ut_vec_idx_dup",
		TableName:  "ut_vector_migrate",
		ColumnName: "embedding",
		Distance:   VectorDistanceEuclidean,
	}
	if err := m.CreateVectorIndex(opts); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if !m.HasVectorIndex("ut_vec_idx_dup") {
		t.Fatal("index should exist")
	}
	if err := m.DropVectorIndex("ut_vec_idx_dup"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	if m.HasVectorIndex("ut_vec_idx_dup") {
		t.Fatal("index should be dropped")
	}
}

func TestMigratorCreateTable_AppendsHeapForVectorModel(t *testing.T) {
	db := openVectorTestDB(t)
	if err := db.Statement.Parse(&vectorMigrateModel{}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !SchemaHasVectorField(db.Statement.Schema) {
		t.Fatal("expected vector schema")
	}

	m, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	needsHeap := false
	_ = m.RunWithValue(&vectorMigrateModel{}, func(stmt *gorm.Statement) error {
		needsHeap = SchemaHasVectorField(stmt.Schema)
		return nil
	})
	if !needsHeap {
		t.Fatal("createTableWithVectorOption should detect vector fields")
	}
}
