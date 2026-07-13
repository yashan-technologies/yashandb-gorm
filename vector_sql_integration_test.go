package yasdb

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"gorm.io/gorm"
)

type vectorSQLProbeModel struct {
	ID        int `gorm:"primaryKey"`
	Embedding Vector `gorm:"type:vector(3,float32);not null"`
}

func (vectorSQLProbeModel) TableName() string {
	return "ut_vector_sql_probe"
}

func setupVectorSQLProbeTable(t *testing.T, db *gorm.DB) {
	t.Helper()
	_ = db.Exec("DROP TABLE ut_vector_sql_probe CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE ut_vector_sql_probe CASCADE CONSTRAINTS").Error
	})
	if err := db.AutoMigrate(&vectorSQLProbeModel{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	row := vectorSQLProbeModel{
		ID:        1,
		Embedding: NewVectorFloat32([]float32{3, 4, 0}),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
}

func TestVectorSQL_Helpers(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSQLProbeTable(t, db)

	dim, err := VectorDimensionCount(db, &vectorSQLProbeModel{}, "Embedding")
	if err != nil {
		t.Fatalf("dimension count: %v", err)
	}
	if dim != 3 {
		t.Fatalf("dimension=%d want 3", dim)
	}

	format, err := VectorDimensionFormat(db, &vectorSQLProbeModel{}, "Embedding")
	if err != nil {
		t.Fatalf("dimension format: %v", err)
	}
	if !strings.EqualFold(format, "FLOAT32") {
		t.Fatalf("format=%q want FLOAT32", format)
	}

	norm, err := VectorNorm(db, &vectorSQLProbeModel{}, "Embedding")
	if err != nil {
		t.Fatalf("norm: %v", err)
	}
	if math.Abs(norm-5) > 1e-4 {
		t.Fatalf("norm=%v want ~5", norm)
	}

	out, err := FromVectorString(db, &vectorSQLProbeModel{}, FromVectorOptions{
		FieldName: "Embedding",
		Size:      128,
	})
	if err != nil {
		t.Fatalf("from_vector: %v", err)
	}
	if out == "" || !strings.Contains(out, "[") {
		t.Fatalf("unexpected from_vector output: %q", out)
	}
}

func TestVectorSQL_BuildToVectorInsert(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	const table = "ut_vector_sql_tv"
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

	expr, err := BuildToVectorSQL("?", ToVectorOptions{Dimension: 3, Format: "FLOAT32"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := db.Exec(`INSERT INTO `+table+` VALUES (2, `+expr+`)`, "[1, 2, 3]").Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	var cnt int64
	if err := db.Raw(`SELECT COUNT(1) FROM ` + table + ` WHERE id = 2`).Scan(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("expected 1 row, got %d", cnt)
	}
}

func TestVectorSQL_FromVectorReturningClob(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	const table = "ut_vector_sql_clob"
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
	if err := db.Exec(`INSERT INTO ` + table + ` VALUES (1, TO_VECTOR('[1,2,3]', 3, FLOAT32))`).Error; err != nil {
		t.Fatalf("insert: %v", err)
	}

	expr, err := BuildFromVectorSQL("v", FromVectorOptions{Returning: VectorReturningClob})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	var out string
	if err := db.Raw(`SELECT `+expr+` FROM `+table+` WHERE id = 1`).Scan(&out).Error; err != nil {
		t.Fatalf("select clob: %v", err)
	}
	if out == "" {
		t.Fatal("expected non-empty clob output")
	}
}

func TestVectorSQL_ComplexAnalyticsQuery(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)
	setupVectorSQLProbeTable(t, db)

	col := "UT_VECTOR_SQL_PROBE.EMBEDDING"
	normExpr, _ := BuildVectorNormSQL(col)
	dimExpr, _ := BuildVectorDimensionCountSQL(col)
	fmtExpr, _ := BuildVectorDimensionFormatSQL(col)
	fromExpr, _ := BuildFromVectorSQL(col, FromVectorOptions{Returning: VectorReturningVarchar, Size: 256})

	sql := fmt.Sprintf(`SELECT ID, %s AS NORM, %s AS DIM, %s AS FMT, %s AS VEC
FROM UT_VECTOR_SQL_PROBE WHERE ID = 1`, normExpr, dimExpr, fmtExpr, fromExpr)

	var result struct {
		ID   int64
		NORM float64
		DIM  int64
		FMT  string
		VEC  string
	}
	if err := db.Raw(sql).Scan(&result).Error; err != nil {
		t.Fatalf("complex analytics query: %v", err)
	}
	if result.DIM != 3 {
		t.Fatalf("dim=%d want 3", result.DIM)
	}
	if math.Abs(result.NORM-5) > 1e-3 {
		t.Fatalf("norm=%v want ~5", result.NORM)
	}
	if !strings.EqualFold(result.FMT, "FLOAT32") {
		t.Fatalf("format=%q", result.FMT)
	}
	if result.VEC == "" || !strings.Contains(result.VEC, "[") {
		t.Fatalf("vec text empty: %q", result.VEC)
	}
}

func TestVectorSQL_ComplexInsertWithSubqueryToVector(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorDDLUnsupported(t, db)

	const table = "ut_vector_sql_subq"
	_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP TABLE " + table + " CASCADE CONSTRAINTS").Error
	})
	if err := db.Exec(`CREATE TABLE ` + table + ` (
		id INT PRIMARY KEY,
		raw_text VARCHAR(4000),
		v VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.Exec(`INSERT INTO `+table+` (id, raw_text, v) VALUES (1, '[3, 4, 0]', TO_VECTOR('[0, 0, 0]', 3, FLOAT32))`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	toExpr, err := BuildToVectorSQL("(SELECT raw_text FROM "+table+" WHERE id = :1)", ToVectorOptions{Dimension: 3, Format: "FLOAT32"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	updateSQL := fmt.Sprintf(`UPDATE %s SET v = %s WHERE id = 1`, table, toExpr)
	if err := db.Exec(updateSQL, 1).Error; err != nil {
		t.Fatalf("update with subquery to_vector: %v", err)
	}

	var norm float64
	normExpr, _ := BuildVectorNormSQL("v")
	if err := db.Raw(fmt.Sprintf("SELECT %s FROM %s WHERE id = 1", normExpr, table)).Scan(&norm).Error; err != nil {
		t.Fatalf("norm after update: %v", err)
	}
	if math.Abs(norm-5) > 1e-2 {
		t.Fatalf("norm=%v want ~5", norm)
	}
}

func TestAlterVectorIndex_RebuildAndVisible(t *testing.T) {
	db := openVectorTestDB(t)
	skipIfVectorUnsupported(t, db)

	_ = db.Exec("DROP INDEX ut_vec_alter_idx").Error
	_ = db.Exec("DROP TABLE ut_vector_alter_idx CASCADE CONSTRAINTS").Error
	t.Cleanup(func() {
		_ = db.Exec("DROP INDEX ut_vec_alter_idx").Error
		_ = db.Exec("DROP TABLE ut_vector_alter_idx CASCADE CONSTRAINTS").Error
	})

	if err := db.Exec(`CREATE TABLE ut_vector_alter_idx (
		id INT PRIMARY KEY,
		embedding VECTOR(3, FLOAT32) NOT NULL
	) ORGANIZATION HEAP`).Error; err != nil {
		t.Fatalf("create table: %v", err)
	}
	mig, ok := db.Migrator().(Migrator)
	if !ok {
		t.Fatal("expected yasdb migrator")
	}
	if err := mig.CreateVectorIndex(VectorIndexOptions{
		IndexName:  "ut_vec_alter_idx",
		TableName:  "ut_vector_alter_idx",
		ColumnName: "embedding",
		Distance:   VectorDistanceCosine,
		HNSW:       &VectorHNSWParams{M: 16, EFConstruction: 64},
	}); err != nil {
		t.Fatalf("create index: %v", err)
	}

	invisible := false
	if err := mig.AlterVectorIndex(VectorAlterIndexOptions{
		IndexName: "ut_vec_alter_idx",
		Visible:   &invisible,
	}); err != nil {
		t.Fatalf("alter invisible: %v", err)
	}
	if err := mig.AlterVectorIndex(VectorAlterIndexOptions{
		IndexName: "ut_vec_alter_idx",
		Rebuild:   true,
	}); err != nil {
		t.Fatalf("alter rebuild: %v", err)
	}
	visible := true
	if err := mig.AlterVectorIndex(VectorAlterIndexOptions{
		IndexName: "ut_vec_alter_idx",
		Visible:   &visible,
	}); err != nil {
		t.Fatalf("alter visible: %v", err)
	}
}
