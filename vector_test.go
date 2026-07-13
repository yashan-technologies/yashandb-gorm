package yasdb

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"gorm.io/gorm/schema"
)

func TestIsVectorDataType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"vector(10,float32)", true},
		{"VECTOR(3, FLOAT32)", true},
		{"vector(5)", true},
		{"vector(8,float64)", true},
		{"vector(6,f32)", true},
		{"vector(4,f64)", true},
		{"vector(4,int8)", false},
		{"vector(4,flex)", false},
		{"vector(3,float16)", false},
		{"varchar(32)", false},
		{"", false},
		{"vector()", false},
		{"vector(abc,float32)", false},
	}
	for _, tc := range cases {
		if got := IsVectorDataType(tc.in); got != tc.want {
			t.Errorf("IsVectorDataType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestIsSupportedVectorSQLFormat(t *testing.T) {
	supported := []string{"float32", "FLOAT32", "f32", "float64", "f64", "FLOAT64"}
	for _, f := range supported {
		if !IsSupportedVectorSQLFormat(f) {
			t.Errorf("expected supported: %q", f)
		}
	}
	unsupported := []string{"int8", "i8", "INT8", "flex", "FLEX", "float16", "f16", "customfmt"}
	for _, f := range unsupported {
		if IsSupportedVectorSQLFormat(f) {
			t.Errorf("expected unsupported: %q", f)
		}
	}
}

func TestParseVectorSpec(t *testing.T) {
	spec, ok := ParseVectorSpec("vector(10,float32)")
	if !ok {
		t.Fatal("expected ok")
	}
	if spec.Dimension != 10 || spec.Format != "FLOAT32" {
		t.Fatalf("unexpected spec: %+v", spec)
	}

	spec, ok = ParseVectorSpec("VECTOR(5, FLOAT64)")
	if !ok || spec.Dimension != 5 || spec.Format != "FLOAT64" {
		t.Fatalf("unexpected float64 spec: %+v ok=%v", spec, ok)
	}

	spec, ok = ParseVectorSpec("vector(7)")
	if !ok || spec.Dimension != 7 || spec.Format != "FLOAT32" {
		t.Fatalf("default format spec: %+v ok=%v", spec, ok)
	}

	_, ok = ParseVectorSpec("blob")
	if ok {
		t.Fatal("blob should not parse as vector")
	}

	unsupported := []string{
		"vector(4,int8)",
		"vector(2,i8)",
		"vector(4,flex)",
		"vector(3,float16)",
		"vector(3,customfmt)",
	}
	for _, in := range unsupported {
		if _, ok := ParseVectorSpec(in); ok {
			t.Errorf("ParseVectorSpec(%q) should fail", in)
		}
	}
}

func TestNormalizeVectorSQLType(t *testing.T) {
	cases := map[string]string{
		"vector(10,float32)": "VECTOR(10, FLOAT32)",
		"VECTOR(3, float64)": "VECTOR(3, FLOAT64)",
		"vector(6,f32)":      "VECTOR(6, FLOAT32)",
		"vector(2,f64)":      "VECTOR(2, FLOAT64)",
		"vector(5)":          "VECTOR(5, FLOAT32)",
	}
	for in, want := range cases {
		got, ok := NormalizeVectorSQLType(in)
		if !ok || got != want {
			t.Errorf("NormalizeVectorSQLType(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}

	unsupported := []string{
		"int",
		"vector(2,int8)",
		"vector(4,flex)",
		"vector(3,float16)",
	}
	for _, in := range unsupported {
		if _, ok := NormalizeVectorSQLType(in); ok {
			t.Errorf("NormalizeVectorSQLType(%q) should fail", in)
		}
	}
}

func TestNewVectorConstructors(t *testing.T) {
	f32 := NewVectorFloat32([]float32{1, 2, 3})
	if f32.Dim != 3 || f32.Format != VectorFormatFloat32 {
		t.Fatalf("unexpected float32 vector: %+v", f32)
	}
	if f32.String() != "[1, 2, 3]" {
		t.Fatalf("unexpected string: %q", f32.String())
	}

	f64 := NewVectorFloat64([]float64{1.5, 2.5})
	if f64.Dim != 2 || f64.Format != VectorFormatFloat64 {
		t.Fatalf("unexpected float64 vector: %+v", f64)
	}

	i8 := NewVectorInt8([]int8{1, -1})
	if i8.Dim != 2 || i8.Format != VectorFormatInt8 {
		t.Fatalf("unexpected int8 vector: %+v", i8)
	}

	val, err := f32.Value()
	if err != nil {
		t.Fatalf("Value: %v", err)
	}
	if val != "[1, 2, 3]" {
		t.Fatalf("unexpected Value: %v", val)
	}

	var empty Vector
	if s := empty.String(); s != "" {
		t.Fatalf("empty vector string should be empty, got %q", s)
	}
}

func TestDialectorDataTypeOfVector(t *testing.T) {
	d := Dialector{Config: &Config{}}

	field := &schema.Field{
		DataType:  schema.DataType("vector(10,float32)"),
		FieldType: reflect.TypeOf(Vector{}),
	}
	if got := d.DataTypeOf(field); got != "VECTOR(10, FLOAT32)" {
		t.Fatalf("DataTypeOf vector tag: got %q", got)
	}

	field64 := &schema.Field{
		DataType:  schema.DataType("vector(4,float64)"),
		FieldType: reflect.TypeOf(Vector{}),
	}
	if got := d.DataTypeOf(field64); got != "VECTOR(4, FLOAT64)" {
		t.Fatalf("DataTypeOf float64 vector: got %q", got)
	}

	varcharField := &schema.Field{
		DataType:  schema.DataType("varchar(32)"),
		FieldType: reflect.TypeOf(""),
		Size:      32,
	}
	if got := d.DataTypeOf(varcharField); got != "varchar(32)" {
		t.Fatalf("DataTypeOf varchar: got %q", got)
	}
}

func TestDialectorDataTypeOf_UnsupportedVectorFormatPanics(t *testing.T) {
	d := Dialector{Config: &Config{}}
	unsupported := []string{
		"vector(2,int8)",
		"vector(4,flex)",
		"vector(3,float16)",
	}
	for _, dataType := range unsupported {
		t.Run(dataType, func(t *testing.T) {
			field := &schema.Field{
				DataType:  schema.DataType(dataType),
				FieldType: reflect.TypeOf(Vector{}),
			}
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for %q", dataType)
				}
			}()
			_ = d.DataTypeOf(field)
		})
	}
}

func TestNormalizeVectorFormatAliases(t *testing.T) {
	got, ok := NormalizeVectorSQLType("vector(1,f64)")
	if !ok || got != "VECTOR(1, FLOAT64)" {
		t.Fatalf("f64 alias: got (%q, %v)", got, ok)
	}
	got, ok = NormalizeVectorSQLType("vector(2,f32)")
	if !ok || got != "VECTOR(2, FLOAT32)" {
		t.Fatalf("f32 alias: got (%q, %v)", got, ok)
	}
	if _, ok := NormalizeVectorSQLType("vector(2,i8)"); ok {
		t.Fatal("i8 alias should be rejected")
	}
}

func TestVectorScan(t *testing.T) {
	var v Vector
	if err := v.Scan(nil); err != nil {
		t.Fatalf("scan nil: %v", err)
	}
	src := NewVectorFloat32([]float32{1, 2})
	if err := v.Scan(src); err != nil {
		t.Fatalf("scan value: %v", err)
	}
	if v.Dim != 2 {
		t.Fatalf("unexpected dim after scan: %d", v.Dim)
	}
	ptr := &src
	if err := v.Scan(ptr); err != nil {
		t.Fatalf("scan pointer: %v", err)
	}
	if err := v.Scan("not-a-vector"); err == nil {
		t.Fatal("expected scan error for unsupported type")
	}
}

func TestParseVectorSpec_InvalidDimension(t *testing.T) {
	if _, ok := ParseVectorSpec("vector(65535,float32)"); !ok {
		t.Fatal("max uint16 dimension should parse")
	}
	if _, ok := ParseVectorSpec("vector(0)"); !ok {
		t.Fatal("zero dimension should parse")
	}
	if _, ok := ParseVectorSpec("vector(70000,float32)"); ok {
		t.Fatal("dimension overflow uint16 should not parse")
	}
}

func TestDataTypeOf_VectorDoesNotAppendStringDefault(t *testing.T) {
	d := Dialector{Config: &Config{}}
	field := &schema.Field{
		DataType:  schema.DataType("vector(8,float32)"),
		FieldType: reflect.TypeOf(Vector{}),
		TagSettings: map[string]string{
			"DEFAULT": "''",
		},
	}
	got := d.DataTypeOf(field)
	if got != "VECTOR(8, FLOAT32)" {
		t.Fatalf("vector should not get string default suffix: %q", got)
	}
	if strings.Contains(got, "DEFAULT") {
		t.Fatalf("vector DDL must not include DEFAULT: %q", got)
	}
}

func TestLooksLikeVectorDataType(t *testing.T) {
	if !looksLikeVectorDataType("vector(3,int8)") {
		t.Fatal("should look like vector syntax")
	}
	if looksLikeVectorDataType("varchar(32)") {
		t.Fatal("varchar should not match")
	}
}

func TestComposeVectorDDL_MultiFormatNormalization_Complex(t *testing.T) {
	specs := []struct {
		input string
		want  string
	}{
		{"vector(768,float32)", "VECTOR(768, FLOAT32)"},
		{"vector(1536,f64)", "VECTOR(1536, FLOAT64)"},
		{"vector(256,FLOAT32)", "VECTOR(256, FLOAT32)"},
	}
	var cols []string
	for i, s := range specs {
		got, ok := NormalizeVectorSQLType(s.input)
		if !ok || got != s.want {
			t.Fatalf("normalize %q: got %q ok=%v", s.input, got, ok)
		}
		cols = append(cols, fmt.Sprintf("v%d %s", i, got))
	}
	ddl := fmt.Sprintf("CREATE TABLE MULTI_VEC (%s)%s", strings.Join(cols, ", "), VectorHeapTableOption())
	if !strings.Contains(ddl, "v0 VECTOR(768, FLOAT32)") || !strings.Contains(ddl, "v2 VECTOR(256, FLOAT32)") {
		t.Fatalf("unexpected ddl: %s", ddl)
	}
}
