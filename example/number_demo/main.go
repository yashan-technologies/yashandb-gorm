package main

import (
	"fmt"

	yasdriver "github.com/yashan-technologies/yashandb-go"
	yasdb "github.com/yashan-technologies/yashandb-gorm"
	"gorm.io/gorm"
)

// NumberDemo 演示 NUMBER 类型的使用
// number_as_string=1 时，NUMBER 值以字符串形式返回
type NumberDemo struct {
	ID       uint              `gorm:"primaryKey"`
	Name     string            `gorm:"type:varchar(64)"`
	Nint64   *int64            `gorm:"type:number"`
	Nfloat64 *float64          `gorm:"type:number"`
	Nstring  *string           `gorm:"type:number"`
	NNumber  *yasdriver.Number `gorm:"type:number"`
}

func int64Ptr(v int64) *int64                        { return &v }
func float64Ptr(v float64) *float64                  { return &v }
func stringPtr(v string) *string                     { return &v }
func numberPtr(v yasdriver.Number) *yasdriver.Number { return &v }

func main() {
	dsn := "regress/regress@172.16.90.80:1688?number_as_string=1"
	db, err := gorm.Open(yasdb.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	// 清理旧表
	db.Exec("DROP TABLE IF EXISTS number_demos")

	// 自动建表
	if err := db.Debug().AutoMigrate(&NumberDemo{}); err != nil {
		panic(err)
	}

	// 插入一行数据
	record := &NumberDemo{
		Name:     "test_number",
		Nint64:   int64Ptr(987654321111),
		Nfloat64: float64Ptr(0.123456789),
		Nstring:  stringPtr("999999999.999999999"),
		NNumber:  numberPtr(yasdriver.NewNumber("3.14159")),
	}
	if err := db.Debug().Create(record).Error; err != nil {
		panic(err)
	}
	fmt.Printf("插入成功: %+v\n", record)

	// 查询数据
	var result NumberDemo
	if err := db.Debug().Where("name = ?", "test_number").First(&result).Error; err != nil {
		panic(err)
	}
	fmt.Printf("查询结果: ID=%d, Name=%s, Nint64=%d, Nfloat64=%f, Nstring=%s\n",
		result.ID, result.Name, *result.Nint64, *result.Nfloat64, *result.Nstring)
	nf, _ := result.NNumber.Float64()
	fmt.Println("Number:", nf, result.NNumber.String())

	// 多行插入，含空值场景
	records := []NumberDemo{
		{
			Name:     "all_values",
			Nint64:   int64Ptr(1234567890),
			Nfloat64: float64Ptr(3.14159265),
			Nstring:  stringPtr("12345.6789"),
			NNumber:  numberPtr(yasdriver.NewNumber("99.99")),
		},
		{
			Name:     "null_int64",
			Nint64:   nil,
			Nfloat64: float64Ptr(1.5),
			Nstring:  stringPtr("100"),
			NNumber:  numberPtr(yasdriver.NewNumber("2.71")),
		},
		{
			Name:     "null_float64",
			Nint64:   int64Ptr(100),
			Nfloat64: nil,
			Nstring:  stringPtr("200.5"),
			NNumber:  numberPtr(yasdriver.NewNumber("1.23")),
		},
		{
			Name:     "null_string",
			Nint64:   int64Ptr(200),
			Nfloat64: float64Ptr(2.5),
			Nstring:  nil,
			NNumber:  numberPtr(yasdriver.NewNumber("4.56")),
		},
		{
			Name:     "null_number",
			Nint64:   int64Ptr(300),
			Nfloat64: float64Ptr(3.5),
			Nstring:  stringPtr("400"),
			NNumber:  &yasdriver.Number{},
		},
		// 边界值数据
		{
			Name:     "zero",
			Nint64:   int64Ptr(0),
			Nfloat64: float64Ptr(0.0),
			Nstring:  stringPtr("0"),
			NNumber:  numberPtr(yasdriver.NewNumber("0")),
		},
		{
			Name:     "negative",
			Nint64:   int64Ptr(-9999999999),
			Nfloat64: float64Ptr(-123.456),
			Nstring:  stringPtr("-99999.99999"),
			NNumber:  numberPtr(yasdriver.NewNumber("-3.14159")),
		},
		{
			Name:     "max_int64",
			Nint64:   int64Ptr(9223372036854775807), // int64最大值 (19位)
			Nfloat64: float64Ptr(9.99999999999999e+37),
			Nstring:  stringPtr("9223372036854775807"),
			NNumber:  numberPtr(yasdriver.NewNumber("9223372036854775807")),
		},
		{
			Name:     "min_int64",
			Nint64:   int64Ptr(-9223372036854775808), // int64最小值 (19位)
			Nfloat64: float64Ptr(-9.99999999999999e+37),
			Nstring:  stringPtr("-9223372036854775808"),
			NNumber:  numberPtr(yasdriver.NewNumber("-9223372036854775808")),
		},
		{
			Name:     "max_number_38digit",
			Nint64:   nil,
			Nfloat64: nil,
			Nstring:  stringPtr("99999999999999999999999999999999999999"), // 38位最大整数
			NNumber:  numberPtr(yasdriver.NewNumber("99999999999999999999999999999999999999")),
		},
		{
			Name:     "min_number_38digit",
			Nint64:   nil,
			Nfloat64: nil,
			Nstring:  stringPtr("-99999999999999999999999999999999999999"), // 38位最小整数
			NNumber:  numberPtr(yasdriver.NewNumber("-99999999999999999999999999999999999999")),
		},
		{
			Name:     "max_precision",
			Nint64:   nil,
			Nfloat64: nil,
			Nstring:  stringPtr("99999999999999999999999999999999999999.9999999999999999999999999"), // 38+25位最大精度
			NNumber:  numberPtr(yasdriver.NewNumber("99999999999999999999999999999999999999.9999999999999999999999999")),
		},
		{
			Name:     "smallest_decimal",
			Nint64:   nil,
			Nfloat64: nil,
			Nstring:  stringPtr("0.0000000000000000000000001"), // 25位最小正小数
			NNumber:  numberPtr(yasdriver.NewNumber("0.0000000000000000000000001")),
		},
		{
			Name:     "all_null",
			Nint64:   nil,
			Nfloat64: nil,
			Nstring:  nil,
			NNumber:  nil,
		},
	}

	if err := db.Debug().Create(&records).Error; err != nil {
		panic(err)
	}
	fmt.Printf("\n多行插入成功，共 %d 条\n", len(records))

	// 查询所有数据
	var results []NumberDemo
	if err := db.Debug().Order("id").Find(&results).Error; err != nil {
		panic(err)
	}

	fmt.Println("\n全部数据:")
	for _, r := range results {
		fmt.Printf("ID=%d, Name=%-14s", r.ID, r.Name)
		if r.Nint64 != nil {
			fmt.Printf(", Nint64=%d", *r.Nint64)
		} else {
			fmt.Printf(", Nint64=NULL")
		}
		if r.Nfloat64 != nil {
			fmt.Printf(", Nfloat64=%f", *r.Nfloat64)
		} else {
			fmt.Printf(", Nfloat64=NULL")
		}
		if r.Nstring != nil {
			fmt.Printf(", Nstring=%s", *r.Nstring)
		} else {
			fmt.Printf(", Nstring=NULL")
		}
		if r.NNumber != nil {
			fmt.Printf(", NNumber=%s", r.NNumber.String())
		} else {
			fmt.Printf(", NNumber=NULL")
		}
		fmt.Println()
	}
}
