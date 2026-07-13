package yasdb

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	yasdbdriver "github.com/yashan-technologies/yashandb-go"
	"gorm.io/gorm/schema"
)

// Vector 复用 yashandb-go 驱动向量类型，绑定与扫描由驱动层完成。
type Vector = yasdbdriver.Vector

// VectorFormat 向量元素格式。
type VectorFormat = yasdbdriver.VectorFormat

const (
	VectorFormatFlex    = yasdbdriver.VectorFormatFlex
	VectorFormatFloat16 = yasdbdriver.VectorFormatFloat16
	VectorFormatFloat32 = yasdbdriver.VectorFormatFloat32
	VectorFormatFloat64 = yasdbdriver.VectorFormatFloat64
	VectorFormatInt8    = yasdbdriver.VectorFormatInt8
)

type VectorSpec struct {
	Dimension uint16
	Format    string
}

var vectorTypeRE = regexp.MustCompile(`(?i)^\s*vector\s*\(\s*(\d+)\s*(?:,\s*(\w+)\s*)?\)\s*$`)

const vectorHeapTableOption = " ORGANIZATION HEAP"

// VectorHeapTableOption 返回向量表建表后缀（ORGANIZATION HEAP）。
func VectorHeapTableOption() string {
	return vectorHeapTableOption
}

// SchemaHasVectorField 判断 schema 是否包含向量列。
func SchemaHasVectorField(s *schema.Schema) bool {
	if s == nil {
		return false
	}
	vectorType := reflect.TypeOf(Vector{})
	for _, field := range s.Fields {
		if field == nil {
			continue
		}
		if IsVectorDataType(string(field.DataType)) {
			return true
		}
		if field.FieldType == vectorType {
			return true
		}
	}
	return false
}

// mergeVectorTableOption 在已有 table_options 上合并 HEAP 组织方式。
func mergeVectorTableOption(current string, needsHeap bool) string {
	if !needsHeap {
		return current
	}
	upper := strings.ToUpper(current)
	if strings.Contains(upper, "ORGANIZATION") {
		return current
	}
	return current + vectorHeapTableOption
}

// NewVectorFloat32 构造 FLOAT32 向量值。
func NewVectorFloat32(data []float32) Vector {
	return Vector{
		Data:   data,
		Dim:    uint16(len(data)),
		Format: VectorFormatFloat32,
	}
}

// NewVectorFloat64 构造 FLOAT64 向量值。
func NewVectorFloat64(data []float64) Vector {
	return Vector{
		Data:   data,
		Dim:    uint16(len(data)),
		Format: VectorFormatFloat64,
	}
}

// NewVectorInt8 构造 INT8 向量值（驱动层能力；YashanDB 23.5 VECTOR DDL 不支持 INT8）。
func NewVectorInt8(data []int8) Vector {
	return Vector{
		Data:   data,
		Dim:    uint16(len(data)),
		Format: VectorFormatInt8,
	}
}

// IsVectorDataType 判断 gorm type 是否为向量定义。
func IsVectorDataType(dataType string) bool {
	_, ok := ParseVectorSpec(dataType)
	return ok
}

// ParseVectorSpec 解析 vector(dim[, format]) 定义。
func ParseVectorSpec(dataType string) (VectorSpec, bool) {
	m := vectorTypeRE.FindStringSubmatch(strings.TrimSpace(dataType))
	if m == nil {
		return VectorSpec{}, false
	}

	dim, err := strconv.ParseUint(m[1], 10, 16)
	if err != nil {
		return VectorSpec{}, false
	}

	format := "FLOAT32"
	if len(m[2]) > 0 {
		normalized, ok := normalizeVectorFormat(m[2])
		if !ok {
			return VectorSpec{}, false
		}
		format = normalized
	}

	return VectorSpec{
		Dimension: uint16(dim),
		Format:    format,
	}, true
}

// IsSupportedVectorSQLFormat 判断向量元素格式是否为 YashanDB 当前支持的 DDL 格式。
func IsSupportedVectorSQLFormat(format string) bool {
	_, ok := normalizeVectorFormat(format)
	return ok
}

// NormalizeVectorSQLType 将 gorm type tag 规范为 YashanDB VECTOR DDL。
func NormalizeVectorSQLType(dataType string) (string, bool) {
	spec, ok := ParseVectorSpec(dataType)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("VECTOR(%d, %s)", spec.Dimension, spec.Format), true
}

func normalizeVectorFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "float32", "f32":
		return "FLOAT32", true
	case "float64", "f64":
		return "FLOAT64", true
	default:
		return "", false
	}
}

// looksLikeVectorDataType 判断字符串是否为 vector(dim[,format]) 形态（不校验格式是否受支持）。
func looksLikeVectorDataType(dataType string) bool {
	return vectorTypeRE.MatchString(strings.TrimSpace(dataType))
}
