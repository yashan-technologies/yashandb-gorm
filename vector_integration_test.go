package yasdb

import (
	"math"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const vectorTestDSN = "MY_TEST008/123456@172.16.90.67:23500"

type vectorTestModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorTestModel) TableName() string {
	return "ut_vector_item"
}

type vectorFloat64TestModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(2,float64);not null"`
}

func (vectorFloat64TestModel) TableName() string {
	return "ut_vector_f64"
}

func openVectorTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(Open(vectorTestDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

// skipIfVectorDDLUnsupported 探测向量 DDL 能力（不依赖 Go Vector 绑定）。
func skipIfVectorDDLUnsupported(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP TABLE ut_vector_ddl_probe CASCADE CONSTRAINTS").Error
	if err := db.Exec(`CREATE TABLE ut_vector_ddl_probe (
		id INT PRIMARY KEY,
		v VECTOR(1, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Skipf("vector DDL not supported by database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_ddl_probe CASCADE CONSTRAINTS").Error
	})
}

// skipIfVectorUnsupported 探测向量绑定能力；C 客户端需 23.5+（含 yacDescAlloc2 符号）。
func skipIfVectorUnsupported(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP TABLE ut_vector_probe CASCADE CONSTRAINTS").Error
	if err := db.Exec(`CREATE TABLE ut_vector_probe (
		id INT PRIMARY KEY,
		v VECTOR(1, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Skipf("vector DDL not supported by database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_probe CASCADE CONSTRAINTS").Error
	})
	if err := db.Exec("INSERT INTO ut_vector_probe VALUES (1, ?)", NewVectorFloat32([]float32{1})).Error; err != nil {
		t.Skipf("vector bind not supported (upgrade yashandb-client to 23.5+): %v", err)
	}
	_ = db.Exec("DELETE FROM ut_vector_probe WHERE id = 1").Error
}

func setupVectorFloat32Table(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP TABLE ut_vector_item CASCADE CONSTRAINTS").Error
	if err := db.Exec(`CREATE TABLE ut_vector_item (
		id INT PRIMARY KEY,
		embedding VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create vector table: %v", err)
	}
}

func setupVectorFloat64Table(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP TABLE ut_vector_f64 CASCADE CONSTRAINTS").Error
	if err := db.Exec(`CREATE TABLE ut_vector_f64 (
		id INT PRIMARY KEY,
		embedding VECTOR(2, FLOAT64) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create float64 vector table: %v", err)
	}
}

func TestVectorFloat32_CreateFindUpdateDelete(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorFloat32Table(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_item CASCADE CONSTRAINTS").Error
	})

	orig := NewVectorFloat32([]float32{0.1, 0.2, 0.3})
	row := vectorTestModel{ID: 1, Embedding: orig}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var found vectorTestModel
	if err := db.First(&found, 1).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	data, ok := found.Embedding.Data.([]float32)
	if !ok {
		t.Fatalf("expected []float32, got %T", found.Embedding.Data)
	}
	if len(data) != 3 {
		t.Fatalf("unexpected dim: %d", len(data))
	}
	for i, want := range []float32{0.1, 0.2, 0.3} {
		if math.Abs(float64(data[i]-want)) > 1e-5 {
			t.Fatalf("data[%d]=%g want %g", i, data[i], want)
		}
	}

	updated := NewVectorFloat32([]float32{1, 2, 3})
	if err := db.Model(&vectorTestModel{}).Where("id = ?", 1).Update("embedding", updated).Error; err != nil {
		t.Fatalf("update: %v", err)
	}

	var after vectorTestModel
	if err := db.First(&after, 1).Error; err != nil {
		t.Fatalf("first after update: %v", err)
	}
	updData := after.Embedding.Data.([]float32)
	if updData[0] < 0.99 || updData[2] < 2.99 {
		t.Fatalf("unexpected updated vector: %+v", updData)
	}

	if err := db.Delete(&vectorTestModel{}, 1).Error; err != nil {
		t.Fatalf("delete: %v", err)
	}
	var none vectorTestModel
	if err := db.First(&none, 1).Error; err == nil {
		t.Fatal("expected record not found after delete")
	}
}

func TestVectorFloat32_BatchCreateAndFind(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorFloat32Table(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_item CASCADE CONSTRAINTS").Error
	})

	rows := []vectorTestModel{
		{ID: 10, Embedding: NewVectorFloat32([]float32{1, 0, 0})},
		{ID: 11, Embedding: NewVectorFloat32([]float32{0, 1, 0})},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("batch create: %v", err)
	}

	var all []vectorTestModel
	if err := db.Order("id").Find(&all).Error; err != nil {
		t.Fatalf("find: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(all))
	}
}

func TestVectorFloat64_RoundTrip(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorFloat64Table(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_f64 CASCADE CONSTRAINTS").Error
	})

	vec := NewVectorFloat64([]float64{1.25, 2.75})
	if err := db.Create(&vectorFloat64TestModel{ID: 1, Embedding: vec}).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var found vectorFloat64TestModel
	if err := db.First(&found, 1).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	data, ok := found.Embedding.Data.([]float64)
	if !ok {
		t.Fatalf("expected []float64, got %T", found.Embedding.Data)
	}
	if math.Abs(data[0]-1.25) > 1e-9 || math.Abs(data[1]-2.75) > 1e-9 {
		t.Fatalf("unexpected float64 vector: %+v", data)
	}
}

func TestVectorFloat32_Transaction(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorFloat32Table(t, db)
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_item CASCADE CONSTRAINTS").Error
	})

	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&vectorTestModel{ID: 20, Embedding: NewVectorFloat32([]float32{9, 8, 7})}).Error; err != nil {
			return err
		}
		return tx.Model(&vectorTestModel{}).Where("id = ?", 20).
			Update("embedding", NewVectorFloat32([]float32{7, 8, 9})).Error
	})
	if err != nil {
		t.Fatalf("transaction: %v", err)
	}

	var found vectorTestModel
	if err := db.First(&found, 20).Error; err != nil {
		t.Fatalf("first: %v", err)
	}
	data := found.Embedding.Data.([]float32)
	if data[0] < 6.99 {
		t.Fatalf("unexpected tx result: %+v", data)
	}
}

func TestVectorSchema_ParseAndDataType(t *testing.T) {
	db := openVectorTestDB(t)
	if err := db.Statement.Parse(&vectorTestModel{}); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	field := db.Statement.Schema.LookUpField("Embedding")
	if field == nil {
		t.Fatal("embedding field not found")
	}
	d := Dialector{Config: &Config{}}
	if got := d.DataTypeOf(field); got != "VECTOR(3, FLOAT32)" {
		t.Fatalf("schema DataTypeOf: got %q", got)
	}
}
