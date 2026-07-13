package yasdb

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// --- 复杂场景测试用模型 ---

type vectorSQLComplexModel struct {
	ID        int64  `gorm:"primaryKey"`
	Title     string `gorm:"column:doc_title"`
	Embedding Vector `gorm:"column:doc_embedding;type:vector(768,float64);not null"`
	QueryVec  Vector `gorm:"column:query_vec;type:vector(768,float64)"`
}

func (vectorSQLComplexModel) TableName() string {
	return "doc_vector_store"
}

func openDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(Open(vectorTestDSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db.Session(&gorm.Session{DryRun: true})
}

func buildVectorHelperSelectSQL(t *testing.T, db *gorm.DB, model interface{}, fieldName, columnName string, build func(string) (string, error)) string {
	t.Helper()
	stmt, err := parseModelStmt(db, model)
	if err != nil {
		t.Fatalf("parse model: %v", err)
	}
	col, err := resolveVectorColumnRef(stmt, fieldName, columnName)
	if err != nil {
		t.Fatalf("resolve column: %v", err)
	}
	expr, err := build(col)
	if err != nil {
		t.Fatalf("build expr: %v", err)
	}
	return fmt.Sprintf("SELECT %s FROM %s WHERE ID = :1", expr, ConvertNameToFormat(stmt.Table))
}

func TestBuildToVectorSQL(t *testing.T) {
	sql, err := BuildToVectorSQL("?", ToVectorOptions{Dimension: 3, Format: "float32"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "TO_VECTOR(?, 3, FLOAT32)"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}

	sql, err = BuildToVectorSQL("'[1,2,3]'", ToVectorOptions{Dimension: 3})
	if err != nil {
		t.Fatalf("build default format: %v", err)
	}
	if sql != "TO_VECTOR('[1,2,3]', 3, FLOAT32)" {
		t.Fatalf("unexpected sql: %s", sql)
	}

	_, err = BuildToVectorSQL("", ToVectorOptions{Dimension: 3})
	if err == nil {
		t.Fatal("expected error for empty expr")
	}
	_, err = BuildToVectorSQL("?", ToVectorOptions{Dimension: 0})
	if err == nil {
		t.Fatal("expected error for zero dimension")
	}
	_, err = BuildToVectorSQL("?", ToVectorOptions{Dimension: 3, Format: "int8"})
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
}

func TestBuildToVectorSQL_ComplexExprs(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		opts    ToVectorOptions
		contain []string
	}{
		{
			name: "subquery string",
			expr: "(SELECT raw_text FROM staging WHERE id = :1)",
			opts: ToVectorOptions{Dimension: 768, Format: "FLOAT32"},
			contain: []string{
				"TO_VECTOR((SELECT raw_text FROM staging WHERE id = :1), 768, FLOAT32)",
			},
		},
		{
			name: "cast clob column",
			expr: "CAST(payload AS VARCHAR(32767))",
			opts: ToVectorOptions{Dimension: 1536, Format: "f64"},
			contain: []string{
				"TO_VECTOR(CAST(payload AS VARCHAR(32767)), 1536, FLOAT64)",
			},
		},
		{
			name: "nested to_vector adjust format",
			expr: "TO_VECTOR(:1, 128, FLOAT32)",
			opts: ToVectorOptions{Dimension: 128, Format: "FLOAT64"},
			contain: []string{
				"TO_VECTOR(TO_VECTOR(:1, 128, FLOAT32), 128, FLOAT64)",
			},
		},
		{
			name: "table qualified column",
			expr: "doc_vector_store.doc_embedding",
			opts: ToVectorOptions{Dimension: 3, Format: "float32"},
			contain: []string{
				"TO_VECTOR(doc_vector_store.doc_embedding, 3, FLOAT32)",
			},
		},
		{
			name: "large dimension boundary",
			expr: "?",
			opts: ToVectorOptions{Dimension: 65535, Format: "FLOAT32"},
			contain: []string{"TO_VECTOR(?, 65535, FLOAT32)"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := BuildToVectorSQL(tc.expr, tc.opts)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			for _, part := range tc.contain {
				if sql != part && !strings.Contains(sql, part) {
					t.Fatalf("got %q, want contain %q", sql, part)
				}
			}
		})
	}
}

func TestBuildToVectorSQL_ComposedInsertUpdate(t *testing.T) {
	toExpr, err := BuildToVectorSQL("?", ToVectorOptions{Dimension: 768, Format: "FLOAT32"})
	if err != nil {
		t.Fatalf("build to_vector: %v", err)
	}

	insertSQL := fmt.Sprintf(
		`INSERT INTO DOC_VECTOR_STORE (ID, DOC_TITLE, DOC_EMBEDDING) VALUES (:1, :2, %s)`,
		toExpr,
	)
	wantInsert := "INSERT INTO DOC_VECTOR_STORE (ID, DOC_TITLE, DOC_EMBEDDING) VALUES (:1, :2, TO_VECTOR(?, 768, FLOAT32))"
	if insertSQL != wantInsert {
		t.Fatalf("insert sql:\ngot  %q\nwant %q", insertSQL, wantInsert)
	}

	updateSQL := fmt.Sprintf(
		`UPDATE DOC_VECTOR_STORE SET DOC_EMBEDDING = %s, DOC_TITLE = :2 WHERE ID = :1`,
		toExpr,
	)
	if !strings.Contains(updateSQL, "TO_VECTOR(?, 768, FLOAT32)") {
		t.Fatalf("unexpected update sql: %s", updateSQL)
	}

	mergeSQL := fmt.Sprintf(`
MERGE INTO DOC_VECTOR_STORE t
USING (SELECT :1 AS id, :2 AS title, %s AS embedding FROM DUAL) s
ON (t.ID = s.id)
WHEN MATCHED THEN UPDATE SET t.DOC_EMBEDDING = s.embedding, t.DOC_TITLE = s.title
WHEN NOT MATCHED THEN INSERT (ID, DOC_TITLE, DOC_EMBEDDING) VALUES (s.id, s.title, s.embedding)`, toExpr)
	if !strings.Contains(mergeSQL, "TO_VECTOR(?, 768, FLOAT32)") {
		t.Fatalf("merge sql missing to_vector: %s", mergeSQL)
	}
	if !strings.Contains(mergeSQL, "WHEN NOT MATCHED THEN INSERT") {
		t.Fatalf("merge sql incomplete: %s", mergeSQL)
	}
}

func TestBuildFromVectorSQL(t *testing.T) {
	sql, err := BuildFromVectorSQL("EMBEDDING", FromVectorOptions{Returning: VectorReturningVarchar, Size: 4000})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "FROM_VECTOR(EMBEDDING RETURNING VARCHAR(4000))"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}

	sql, err = BuildFromVectorSQL("EMBEDDING", FromVectorOptions{Returning: VectorReturningClob})
	if err != nil {
		t.Fatalf("build clob: %v", err)
	}
	if sql != "FROM_VECTOR(EMBEDDING RETURNING CLOB)" {
		t.Fatalf("unexpected sql: %s", sql)
	}

	_, err = BuildFromVectorSQL("EMBEDDING", FromVectorOptions{Returning: VectorReturningVarchar, Size: 0})
	if err == nil {
		t.Fatal("expected error for invalid varchar size")
	}
	_, err = BuildFromVectorSQL("EMBEDDING", FromVectorOptions{Returning: VectorReturningVarchar, Size: 65535})
	if err == nil {
		t.Fatal("expected error for varchar size > 65534")
	}
}

func TestBuildFromVectorSQL_ComplexExprs(t *testing.T) {
	cases := []struct {
		name string
		col  string
		opts FromVectorOptions
		want string
	}{
		{
			name: "qualified column varchar max",
			col:  "DOC_VECTOR_STORE.DOC_EMBEDDING",
			opts: FromVectorOptions{Returning: VectorReturningVarchar, Size: 65534},
			want: "FROM_VECTOR(DOC_VECTOR_STORE.DOC_EMBEDDING RETURNING VARCHAR(65534))",
		},
		{
			name: "to_vector wrapped expression",
			col:  "TO_VECTOR(:1, 768, FLOAT32)",
			opts: FromVectorOptions{Returning: VectorReturningClob},
			want: "FROM_VECTOR(TO_VECTOR(:1, 768, FLOAT32) RETURNING CLOB)",
		},
		{
			name: "case expression",
			col:  "CASE WHEN DOC_EMBEDDING IS NULL THEN TO_VECTOR('[0,0,0]', 3, FLOAT32) ELSE DOC_EMBEDDING END",
			opts: FromVectorOptions{Returning: VectorReturningVarchar, Size: 8192},
			want: "FROM_VECTOR(CASE WHEN DOC_EMBEDDING IS NULL THEN TO_VECTOR('[0,0,0]', 3, FLOAT32) ELSE DOC_EMBEDDING END RETURNING VARCHAR(8192))",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := BuildFromVectorSQL(tc.col, tc.opts)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if sql != tc.want {
				t.Fatalf("got %q want %q", sql, tc.want)
			}
		})
	}
}

func TestBuildVectorHelperSQL(t *testing.T) {
	cases := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{
			name: "norm",
			got: func() (string, error) { return BuildVectorNormSQL("EMBEDDING") },
			want: "VECTOR_NORM(EMBEDDING)",
		},
		{
			name: "dimension count",
			got: func() (string, error) { return BuildVectorDimensionCountSQL("EMBEDDING") },
			want: "VECTOR_DIMENSION_COUNT(EMBEDDING)",
		},
		{
			name: "dimension format",
			got: func() (string, error) { return BuildVectorDimensionFormatSQL("EMBEDDING") },
			want: "VECTOR_DIMENSION_FORMAT(EMBEDDING)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := tc.got()
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if sql != tc.want {
				t.Fatalf("got %q want %q", sql, tc.want)
			}
		})
	}
}

func TestBuildVectorHelperSQL_ComplexColumnExprs(t *testing.T) {
	cases := []struct {
		name  string
		col   string
		build func(string) (string, error)
		want  string
	}{
		{
			name: "norm on qualified column",
			col:  "DOC_VECTOR_STORE.DOC_EMBEDDING",
			build: BuildVectorNormSQL,
			want:  "VECTOR_NORM(DOC_VECTOR_STORE.DOC_EMBEDDING)",
		},
		{
			name: "dimension on to_vector result",
			col:  "TO_VECTOR(:1, 768, FLOAT64)",
			build: BuildVectorDimensionCountSQL,
			want:  "VECTOR_DIMENSION_COUNT(TO_VECTOR(:1, 768, FLOAT64))",
		},
		{
			name: "format on case expression",
			col:  "CASE WHEN DOC_EMBEDDING IS NOT NULL THEN DOC_EMBEDDING END",
			build: BuildVectorDimensionFormatSQL,
			want:  "VECTOR_DIMENSION_FORMAT(CASE WHEN DOC_EMBEDDING IS NOT NULL THEN DOC_EMBEDDING END)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, err := tc.build(tc.col)
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if sql != tc.want {
				t.Fatalf("got %q want %q", sql, tc.want)
			}
		})
	}
}

func TestComposeVectorAnalyticsSelectSQL(t *testing.T) {
	col := "DOC_VECTOR_STORE.DOC_EMBEDDING"
	normExpr, err := BuildVectorNormSQL(col)
	if err != nil {
		t.Fatalf("norm: %v", err)
	}
	dimExpr, err := BuildVectorDimensionCountSQL(col)
	if err != nil {
		t.Fatalf("dim: %v", err)
	}
	fmtExpr, err := BuildVectorDimensionFormatSQL(col)
	if err != nil {
		t.Fatalf("format: %v", err)
	}
	fromExpr, err := BuildFromVectorSQL(col, FromVectorOptions{Returning: VectorReturningVarchar, Size: 16384})
	if err != nil {
		t.Fatalf("from_vector: %v", err)
	}

	sql := fmt.Sprintf(`SELECT
  ID,
  DOC_TITLE,
  %s AS L2_NORM,
  %s AS DIM,
  %s AS ELEM_FORMAT,
  %s AS EMBEDDING_TEXT
FROM DOC_VECTOR_STORE
WHERE ID IN (SELECT ID FROM DOC_VECTOR_STORE WHERE DOC_TITLE LIKE :1)
  AND %s > :2
ORDER BY ID
FETCH FIRST 100 ROWS ONLY`, normExpr, dimExpr, fmtExpr, fromExpr, normExpr)

	for _, want := range []string{
		"VECTOR_NORM(DOC_VECTOR_STORE.DOC_EMBEDDING) AS L2_NORM",
		"VECTOR_DIMENSION_COUNT(DOC_VECTOR_STORE.DOC_EMBEDDING) AS DIM",
		"VECTOR_DIMENSION_FORMAT(DOC_VECTOR_STORE.DOC_EMBEDDING) AS ELEM_FORMAT",
		"FROM_VECTOR(DOC_VECTOR_STORE.DOC_EMBEDDING RETURNING VARCHAR(16384)) AS EMBEDDING_TEXT",
		"WHERE ID IN (SELECT ID FROM DOC_VECTOR_STORE WHERE DOC_TITLE LIKE :1)",
		"FETCH FIRST 100 ROWS ONLY",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("composed sql missing %q:\n%s", want, sql)
		}
	}
}

func TestComposeVectorSimilarityFilterSQL(t *testing.T) {
	queryExpr, err := BuildToVectorSQL("?", ToVectorOptions{Dimension: 768, Format: "FLOAT64"})
	if err != nil {
		t.Fatalf("query vector: %v", err)
	}
	col := "DOC_VECTOR_STORE.DOC_EMBEDDING"
	sql := fmt.Sprintf(`SELECT ID, DOC_TITLE,
  VECTOR_DISTANCE(%s, %s, COSINE) AS DISTANCE
FROM DOC_VECTOR_STORE
WHERE VECTOR_DIMENSION_COUNT(%s) = 768
  AND VECTOR_DIMENSION_FORMAT(%s) = 'FLOAT64'
ORDER BY DISTANCE
FETCH APPROXIMATE FIRST 20 ROWS ONLY`, col, queryExpr, col, col)

	for _, want := range []string{
		"VECTOR_DISTANCE(DOC_VECTOR_STORE.DOC_EMBEDDING, TO_VECTOR(?, 768, FLOAT64), COSINE)",
		"VECTOR_DIMENSION_COUNT(DOC_VECTOR_STORE.DOC_EMBEDDING) = 768",
		"VECTOR_DIMENSION_FORMAT(DOC_VECTOR_STORE.DOC_EMBEDDING) = 'FLOAT64'",
		"FETCH APPROXIMATE FIRST 20 ROWS ONLY",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("similarity sql missing %q:\n%s", want, sql)
		}
	}
}

func TestResolveVectorColumnRef(t *testing.T) {
	db := openDryRunDB(t)
	stmt, err := parseModelStmt(db, &vectorSQLComplexModel{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	t.Run("by field name qualified", func(t *testing.T) {
		col, err := resolveVectorColumnRef(stmt, "Embedding", "")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if col != "DOC_VECTOR_STORE.DOC_EMBEDDING" {
			t.Fatalf("got %q", col)
		}
	})

	t.Run("by explicit column name", func(t *testing.T) {
		col, err := resolveVectorColumnRef(stmt, "", "query_vec")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if col != "DOC_VECTOR_STORE.QUERY_VEC" {
			t.Fatalf("got %q", col)
		}
	})

	t.Run("column only without table context", func(t *testing.T) {
		col, err := resolveVectorColumnRef(&gorm.Statement{}, "", "doc_embedding")
		if err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if col != "DOC_EMBEDDING" {
			t.Fatalf("got %q", col)
		}
	})

	t.Run("missing field", func(t *testing.T) {
		_, err := resolveVectorColumnRef(stmt, "NotExist", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing field and column", func(t *testing.T) {
		_, err := resolveVectorColumnRef(stmt, "", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestVectorHelperSelectSQL_ComposedFromModel(t *testing.T) {
	db := openDryRunDB(t)
	model := &vectorSQLComplexModel{}

	normSQL := buildVectorHelperSelectSQL(t, db, model, "Embedding", "", BuildVectorNormSQL)
	if normSQL != "SELECT VECTOR_NORM(DOC_VECTOR_STORE.DOC_EMBEDDING) FROM DOC_VECTOR_STORE WHERE ID = :1" {
		t.Fatalf("norm sql: %s", normSQL)
	}

	dimSQL := buildVectorHelperSelectSQL(t, db, model, "", "doc_embedding", BuildVectorDimensionCountSQL)
	if !strings.Contains(dimSQL, "VECTOR_DIMENSION_COUNT(DOC_VECTOR_STORE.DOC_EMBEDDING)") {
		t.Fatalf("dim sql: %s", dimSQL)
	}

	fromSQL := buildVectorHelperSelectSQL(t, db, model, "Embedding", "", func(col string) (string, error) {
		return BuildFromVectorSQL(col, FromVectorOptions{Returning: VectorReturningVarchar, Size: 4000})
	})
	if !strings.Contains(fromSQL, "FROM_VECTOR(DOC_VECTOR_STORE.DOC_EMBEDDING RETURNING VARCHAR(4000))") {
		t.Fatalf("from sql: %s", fromSQL)
	}
}

func TestParseModelStmt_Validation(t *testing.T) {
	db := openDryRunDB(t)
	if _, err := parseModelStmt(nil, &vectorSQLProbeModel{}); err == nil {
		t.Fatal("expected error for nil db")
	}
	if _, err := parseModelStmt(db, nil); err == nil {
		t.Fatal("expected error for nil model")
	}
}

func TestBuildAlterVectorIndexSQL(t *testing.T) {
	visible := true
	invisible := false
	sql, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "hnsw_index",
		Rebuild:   true,
		Visible:   &visible,
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := "ALTER INDEX HNSW_INDEX REBUILD VISIBLE"
	if sql != want {
		t.Fatalf("got %q want %q", sql, want)
	}

	sql, err = BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "hnsw_index",
		Visible:   &invisible,
	})
	if err != nil {
		t.Fatalf("build invisible: %v", err)
	}
	if sql != "ALTER INDEX HNSW_INDEX INVISIBLE" {
		t.Fatalf("unexpected sql: %s", sql)
	}

	_, err = BuildAlterVectorIndexSQL(VectorAlterIndexOptions{IndexName: "idx"})
	if err == nil {
		t.Fatal("expected error when no alter action specified")
	}
}

func TestBuildAlterVectorIndexSQL_ComplexCases(t *testing.T) {
	rebuildOnly, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "ut_vec_search_idx",
		Rebuild:   true,
	})
	if err != nil {
		t.Fatalf("rebuild only: %v", err)
	}
	if rebuildOnly != "ALTER INDEX UT_VEC_SEARCH_IDX REBUILD" {
		t.Fatalf("got %q", rebuildOnly)
	}

	visible := true
	mixedCase, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "hnsw_index",
		Rebuild:   true,
		Visible:   &visible,
	})
	if err != nil {
		t.Fatalf("rebuild+visible: %v", err)
	}
	if mixedCase != "ALTER INDEX HNSW_INDEX REBUILD VISIBLE" {
		t.Fatalf("got %q", mixedCase)
	}

	_, err = BuildAlterVectorIndexSQL(VectorAlterIndexOptions{IndexName: "  "})
	if err == nil {
		t.Fatal("expected error for blank index name")
	}
}

func TestComposeVectorIndexLifecycleDDL(t *testing.T) {
	createSQL, err := BuildCreateVectorIndexSQL(VectorIndexOptions{
		IndexName:  "ut_vec_lifecycle_idx",
		TableName:  "doc_vector_store",
		ColumnName: "doc_embedding",
		Distance:   VectorDistanceCosine,
		HNSW:       &VectorHNSWParams{M: 32, EFConstruction: 128},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	invisible := false
	alterInvisible, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "ut_vec_lifecycle_idx",
		Visible:   &invisible,
	})
	if err != nil {
		t.Fatalf("alter invisible: %v", err)
	}
	alterRebuild, err := BuildAlterVectorIndexSQL(VectorAlterIndexOptions{
		IndexName: "ut_vec_lifecycle_idx",
		Rebuild:   true,
	})
	if err != nil {
		t.Fatalf("alter rebuild: %v", err)
	}
	dropSQL := fmt.Sprintf("DROP INDEX %s", ConvertNameToFormat("ut_vec_lifecycle_idx"))

	script := strings.Join([]string{createSQL, alterInvisible, alterRebuild, dropSQL}, ";\n") + ";"
	for _, want := range []string{
		"CREATE VECTOR INDEX UT_VEC_LIFECYCLE_IDX ON DOC_VECTOR_STORE (DOC_EMBEDDING)",
		"PARAMETERS(TYPE HNSW, M 32, EFCONSTRUCTION 128)",
		"ALTER INDEX UT_VEC_LIFECYCLE_IDX INVISIBLE",
		"ALTER INDEX UT_VEC_LIFECYCLE_IDX REBUILD",
		"DROP INDEX UT_VEC_LIFECYCLE_IDX",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("lifecycle script missing %q:\n%s", want, script)
		}
	}
}

func TestBuildVectorHelperSQL_EmptyColumnErrors(t *testing.T) {
	builders := []func(string) (string, error){
		BuildVectorNormSQL,
		BuildVectorDimensionCountSQL,
		BuildVectorDimensionFormatSQL,
	}
	for _, build := range builders {
		if _, err := build(""); err == nil {
			t.Fatal("expected error for empty column expr")
		}
		if _, err := build("   "); err == nil {
			t.Fatal("expected error for blank column expr")
		}
	}
	if _, err := BuildFromVectorSQL("", FromVectorOptions{Size: 100}); err == nil {
		t.Fatal("expected error for empty from_vector column")
	}
	if _, err := BuildToVectorSQL("  ", ToVectorOptions{Dimension: 3}); err == nil {
		t.Fatal("expected error for blank to_vector expr")
	}
}
