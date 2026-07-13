package yasdb

import (
	"strings"
	"testing"
)

func TestVectorFormat_DDL_Supported(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	cases := []struct {
		name  string
		sql   string
		table string
	}{
		{
			name:  "FLOAT32",
			table: "ut_vec_fmt_f32",
			sql:   `CREATE TABLE ut_vec_fmt_f32 (id INT PRIMARY KEY, v VECTOR(3, FLOAT32) NOT NULL) ORGANIZATION HEAP`,
		},
		{
			name:  "FLOAT64",
			table: "ut_vec_fmt_f64",
			sql:   `CREATE TABLE ut_vec_fmt_f64 (id INT PRIMARY KEY, v VECTOR(2, FLOAT64) NOT NULL) ORGANIZATION HEAP`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = db.Exec("DROP TABLE " + tc.table + " CASCADE CONSTRAINTS").Error
			t.Cleanup(func() {
				_ = db.Exec("DROP TABLE " + tc.table + " CASCADE CONSTRAINTS").Error
			})
			if err := db.Exec(tc.sql).Error; err != nil {
				t.Fatalf("create table: %v", err)
			}
		})
	}
}

func TestVectorFormat_DDL_Unsupported(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	cases := []struct {
		name  string
		sql   string
		table string
	}{
		{
			name:  "INT8",
			table: "ut_vec_fmt_i8",
			sql:   `CREATE TABLE ut_vec_fmt_i8 (id INT PRIMARY KEY, v VECTOR(3, INT8) NOT NULL) ORGANIZATION HEAP`,
		},
		{
			name:  "FLEX",
			table: "ut_vec_fmt_flex",
			sql:   `CREATE TABLE ut_vec_fmt_flex (id INT PRIMARY KEY, v VECTOR(3, FLEX) NOT NULL) ORGANIZATION HEAP`,
		},
		{
			name:  "FLOAT16",
			table: "ut_vec_fmt_f16",
			sql:   `CREATE TABLE ut_vec_fmt_f16 (id INT PRIMARY KEY, v VECTOR(3, FLOAT16) NOT NULL) ORGANIZATION HEAP`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = db.Exec("DROP TABLE " + tc.table + " CASCADE CONSTRAINTS").Error
			err := db.Exec(tc.sql).Error
			if err == nil {
				t.Fatalf("expected DDL error for %s", tc.name)
				_ = db.Exec("DROP TABLE " + tc.table + " CASCADE CONSTRAINTS").Error
			}
			if !strings.Contains(err.Error(), "YAS-04702") && !strings.Contains(strings.ToUpper(err.Error()), "INVALID") {
				t.Fatalf("unexpected error for %s: %v", tc.name, err)
			}
		})
	}
}

func TestVectorFormat_AutoMigrate_OnlySupportedFormats(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	type vectorF32Model struct {
		ID int `gorm:"primaryKey"`
		V  Vector `gorm:"type:vector(2,float32);not null"`
	}
	type vectorUnsupportedModel struct {
		ID int `gorm:"primaryKey"`
		V  Vector `gorm:"type:vector(2,int8);not null"`
	}

	_ = db.Exec("DROP TABLE ut_vec_fmt_mig_f32 CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vec_fmt_mig_f32 CASCADE CONSTRAINTS").Error
	})
	if err := db.AutoMigrate(&vectorF32Model{}); err != nil {
		t.Fatalf("automigrate float32: %v", err)
	}

	_ = db.Exec("DROP TABLE ut_vec_fmt_mig_i8 CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vec_fmt_mig_i8 CASCADE CONSTRAINTS").Error
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("automigrate int8 vector should panic on unsupported format")
		}
	}()
	_ = db.AutoMigrate(&vectorUnsupportedModel{})
}

func TestVectorManual_TO_VECTOR_Insert(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	const table = "ut_vec_manual_tv"
	_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ` + table + ` (
		id INT PRIMARY KEY,
		v VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := db.Exec(`INSERT INTO `+table+` VALUES (
		1, TO_VECTOR('[1, 2, 3]', 3, FLOAT32))`).Error; err != nil {
		t.Fatalf("insert to_vector: %v", err)
	}

	var out string
	if err := db.Raw(`SELECT FROM_VECTOR(v) FROM `+table+` WHERE id = 1`).Scan(&out).Error; err != nil {
		t.Fatalf("from_vector: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty vector string")
	}
}

func TestVectorManual_LiteralStringInsert(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	const table = "ut_vec_manual_lit"
	_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ` + table + ` (
		id INT PRIMARY KEY,
		v VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := db.Exec(`INSERT INTO `+table+` VALUES (1, '[0.1, 0.2, 0.3]')`).Error; err != nil {
		t.Fatalf("insert literal: %v", err)
	}

	var cnt int64
	if err := db.Raw(`SELECT COUNT(1) FROM ` + table).Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 row, got %d", cnt)
	}
}
