package main

import (
    "fmt"

    yasdb "git.yasdb.com/cod-noah/gorm-yasdb"
    "gorm.io/gorm"
)

type Abc struct {
    gorm.Model
    Status string
    Role   string
    Point  int
}

type TaskResult struct {
    RetCode int
    Stdout  string
    Stderr  string
}

func main() {
    dsn := "sys/yasdb_123@192.168.6.177:1688"
    db, err := gorm.Open(yasdb.Open(dsn), &gorm.Config{})
    if err != nil {
        panic(err)
    }

    type Task struct {
        Uuid       string     `json:"uuid"          gorm:"unique;size:64;not null"`
        ParentUuid string     `json:"parentUuid"    gorm:"index;size:64;default:'aaa'"`
        Name       string     `json:"name"          gorm:"index;size:32;not null"`
        Index      string     `json:"index"         gorm:"index;size:128;not null;default:'bbb'"`
        Hostid     string     `json:"hostid"        gorm:"index;size:16;not null"`
        ManageIp   string     `json:"manageIp"      gorm:""`
        HostName   string     `json:"hostName"      gorm:""`
        Result     TaskResult `json:"result"        gorm:"embedded"`
        Mask       string     `json:"mask"          gorm:""`
        Status     int        `json:"status"        gorm:""`
        Progress   int        `json:"progress"      gorm:""`
        Args       []byte     `json:"args"          gorm:"" swaggertype:"object"`
        Depends    []byte     `json:"depends"       gorm:"" swaggertype:"array,string"`
        StartTime  int64      `json:"startTime"     gorm:""`
        EndTime    int64      `json:"endTime"       gorm:""`
        Invisible  int8       `json:"invisible"     gorm:"default:0"`
        Total      int        `json:"-"             gorm:""`
        Finished   int        `json:"-"             gorm:""`
        gorm.Model
    }

    // db.Exec("drop table if exists tasks")
    // db.Exec("create sequence abcs_seq start with 1 increment by 1")
    if err := db.Debug().AutoMigrate(&Task{}); err != nil {
        panic(err)
    }

    err = db.Transaction(func(tx *gorm.DB) error {
        a := []byte(`{"a":1}`)
        t := &Task{
            Uuid:   "aaa",
            Name:   "bbb",
            Hostid: "ccc",
            Args:   a,
        }
        return tx.Debug().Create(&t).Error
    })
    if err != nil {
        panic(err)
    }

    x := &Task{}
    err = db.Debug().Where("uuid = ?", "aaa").First(&x).Error
    if err != nil {
        panic(err)
    }
    fmt.Printf("%+v\n", x)
    fmt.Println(string(x.Args))

    // err = db.Transaction(func(tx *gorm.DB) error {
    //     a := &[]Abc{
    //         {Status: "red", Role: "light", Point: 1},
    //         {Status: "yellow", Role: "light", Point: 2},
    //         {Status: "green", Role: "light", Point: 3},
    //     }
    //     return tx.Debug().Create(&a).Error
    // })
    // if err != nil {
    //     panic(err)
    // }
    //
    // b := []*Abc{}
    // err = db.Debug().Where("point in (?)", []int{1, 2, 3}).Limit(1).Offset(1).Find(&b).Error
    // if err != nil {
    //     panic(err)
    // }

    // fmt.Printf("%+v", b[0])
}
