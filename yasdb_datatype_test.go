package yasdb

import (
	"reflect"
	"testing"

	"gorm.io/gorm/schema"
)

func TestDialectorDataTypeOf_AllTypes(t *testing.T) {
	d := Dialector{Config: &Config{DefaultStringSize: 128}}
	tableSchema := &schema.Schema{Table: "users"}

	tests := []struct {
		name  string
		field *schema.Field
		want  string
	}{
		{
			name: "int",
			field: &schema.Field{
				DataType: schema.Int,
				Schema:   tableSchema,
			},
			want: "BIGINT",
		},
		{
			name: "int autoincrement",
			field: &schema.Field{
				DataType:      schema.Int,
				Schema:        tableSchema,
				AutoIncrement: true,
			},
			want: "BIGINT default SEQUENCE__USERS_.nextval",
		},
		{
			name: "float",
			field: &schema.Field{DataType: schema.Float},
			want: "DOUBLE",
		},
		{
			name: "bool",
			field: &schema.Field{DataType: schema.Bool},
			want: "BOOLEAN",
		},
		{
			name: "string default size",
			field: &schema.Field{DataType: schema.String, Size: 0},
			want: "VARCHAR(128)",
		},
		{
			name: "string explicit size",
			field: &schema.Field{DataType: schema.String, Size: 64},
			want: "VARCHAR(64)",
		},
		{
			name: "string clob",
			field: &schema.Field{DataType: schema.String, Size: 9000},
			want: "CLOB",
		},
		{
			name: "time not null",
			field: &schema.Field{DataType: schema.Time, NotNull: true},
			want: "TIMESTAMP NOT NULL",
		},
		{
			name: "time primary key",
			field: &schema.Field{DataType: schema.Time, PrimaryKey: true},
			want: "TIMESTAMP NOT NULL",
		},
		{
			name: "json",
			field: &schema.Field{DataType: JSON},
			want: "BLOB",
		},
		{
			name: "bytes",
			field: &schema.Field{DataType: schema.Bytes},
			want: "BLOB",
		},
		{
			name: "text alias",
			field: &schema.Field{DataType: schema.DataType("text")},
			want: "CLOB",
		},
		{
			name: "vector tag",
			field: &schema.Field{
				DataType:  schema.DataType("vector(10,float32)"),
				FieldType: reflect.TypeOf(Vector{}),
			},
			want: "VECTOR(10, FLOAT32)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := d.DataTypeOf(tc.field)
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDialectorDataTypeOf_StringDefaultTag(t *testing.T) {
	d := Dialector{Config: &Config{DefaultStringSize: 64}}
	field := &schema.Field{
		DataType: schema.String,
		Size:     32,
		TagSettings: map[string]string{
			"DEFAULT": "''",
		},
	}
	_ = d.DataTypeOf(field)
	if field.DefaultValue != "" {
		t.Fatalf("expected empty default value, got %q", field.DefaultValue)
	}
	if field.DefaultValueInterface != nil {
		t.Fatal("expected nil DefaultValueInterface")
	}
}

func TestDialectorDataTypeOf_CustomDefaultClearsNotNull(t *testing.T) {
	d := Dialector{Config: &Config{}}
	field := &schema.Field{
		DataType: schema.DataType("char(1)"),
		NotNull:  true,
		TagSettings: map[string]string{
			"DEFAULT": "'x'",
		},
	}
	got := d.DataTypeOf(field)
	if got != "char(1)" {
		t.Fatalf("got %q", got)
	}
	if field.NotNull {
		t.Fatal("non-empty DEFAULT should clear NotNull")
	}
}

func TestDialectorDataTypeOf_EmptyPanics(t *testing.T) {
	d := Dialector{Config: &Config{}}
	field := &schema.Field{
		DataType:  schema.DataType(""),
		FieldType: reflect.TypeOf(struct{}{}),
	}
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty sql type")
		}
	}()
	_ = d.DataTypeOf(field)
}

func TestDialectorDataTypeOf_RemovesRestrictTag(t *testing.T) {
	d := Dialector{Config: &Config{}}
	field := &schema.Field{
		DataType: schema.Int,
		TagSettings: map[string]string{
			"RESTRICT": "true",
		},
	}
	_ = d.DataTypeOf(field)
	if _, ok := field.TagSettings["RESTRICT"]; ok {
		t.Fatal("RESTRICT tag should be removed")
	}
}
