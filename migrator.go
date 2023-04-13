package yasdb

import (
    "database/sql"
    "fmt"
    "strings"

    "gorm.io/gorm"
    "gorm.io/gorm/clause"
    "gorm.io/gorm/migrator"
    "gorm.io/gorm/schema"
)

const (
    COLUMNS_HAVE_BEEN_INDEXED = "columns have been indexed"
)

type Migrator struct {
    migrator.Migrator
}

// AutoMigrate auto migrate values
func (m Migrator) AutoMigrate(values ...interface{}) error {
    for _, value := range m.ReorderModels(values, true) {
        tx := m.DB.Session(&gorm.Session{})
        if !tx.Migrator().HasTable(value) {
            if err := tx.Migrator().CreateTable(value); err != nil {
                return err
            }
        } else {
            if err := m.RunWithValue(value, func(stmt *gorm.Statement) (errr error) {
                columnTypes, _ := m.DB.Migrator().ColumnTypes(value)

                for _, dbName := range stmt.Schema.DBNames {
                    field := stmt.Schema.FieldsByDBName[dbName]
                    var foundColumn gorm.ColumnType

                    for _, columnType := range columnTypes {
                        if columnType.Name() == dbName {
                            foundColumn = columnType
                            break
                        }
                    }

                    if foundColumn == nil {
                        // not found, add column
                        if err := tx.Migrator().AddColumn(value, dbName); err != nil {
                            return err
                        }
                    } else if err := m.DB.Migrator().MigrateColumn(value, field, foundColumn); err != nil {
                        // found, smart migrate
                        return err
                    }
                }

                for _, rel := range stmt.Schema.Relationships.Relations {
                    if !m.DB.Config.DisableForeignKeyConstraintWhenMigrating {
                        if constraint := rel.ParseConstraint(); constraint != nil &&
                            constraint.Schema == stmt.Schema && !tx.Migrator().HasConstraint(value, constraint.Name) {
                            if err := tx.Migrator().CreateConstraint(value, constraint.Name); err != nil {
                                return err
                            }
                        }
                    }

                    for _, chk := range stmt.Schema.ParseCheckConstraints() {
                        if !tx.Migrator().HasConstraint(value, chk.Name) {
                            if err := tx.Migrator().CreateConstraint(value, chk.Name); err != nil {
                                return err
                            }
                        }
                    }
                }

                for _, idx := range stmt.Schema.ParseIndexes() {
                    if !tx.Migrator().HasIndex(value, idx.Name) {
                        if err := tx.Migrator().CreateIndex(value, idx.Name); err != nil {
                            if strings.Contains(strings.ToLower(err.Error()), COLUMNS_HAVE_BEEN_INDEXED) {
                                return nil
                            }
                            return err
                        }
                    }
                }

                return nil
            }); err != nil {
                return err
            }
            if err := m.TryQuotifyReservedWords(value); err != nil {
                return err
            }
        }

    }
    return nil
}

func (m Migrator) CurrentDatabase() (name string) {
    m.DB.Raw(
        `SELECT DATABASE_NAME as "Current Database" FROM v$database`,
    ).Row().Scan(&name)
    return
}

func (m Migrator) CreateTable(values ...interface{}) error {
    for _, value := range m.ReorderModels(values, false) {
        m.TryQuotifyReservedWords(value)
        m.TryRemoveOnUpdate(value)
    }
    if err := m.CreateSequence(values); err != nil {
        return err
    }
    return m.Migrator.CreateTable(values...)
}

func (m Migrator) DropTable(values ...interface{}) error {
    values = m.ReorderModels(values, false)
    for i := len(values) - 1; i >= 0; i-- {
        value := values[i]
        tx := m.DB.Session(&gorm.Session{})
        if m.HasTable(value) {
            if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
                return tx.Exec("DROP TABLE ? CASCADE CONSTRAINTS", clause.Table{Name: stmt.Table}).Error
            }); err != nil {
                return err
            }
        }
    }
    return nil
}

func (m Migrator) HasTable(value interface{}) bool {
    var count int64

    m.RunWithValue(value, func(stmt *gorm.Statement) error {
        return m.DB.Raw("SELECT COUNT(1) FROM USER_TABLES WHERE TABLE_NAME = ?", strings.ToUpper(stmt.Table)).Row().Scan(&count)
    })

    return count > 0
}

func (m Migrator) RenameTable(oldName, newName interface{}) (err error) {
    resolveTable := func(name interface{}) (result string, err error) {
        if v, ok := name.(string); ok {
            result = v
        } else {
            stmt := &gorm.Statement{DB: m.DB}
            if err = stmt.Parse(name); err == nil {
                result = stmt.Table
            }
        }
        return
    }

    var oldTable, newTable string

    if oldTable, err = resolveTable(oldName); err != nil {
        return
    }

    if newTable, err = resolveTable(newName); err != nil {
        return
    }

    if !m.HasTable(oldTable) {
        return
    }

    return m.DB.Exec("ALTER TABLE ? RENAME TO ?",
        clause.Table{Name: oldTable},
        clause.Table{Name: newTable},
    ).Error
}

// ColumnTypes return columnTypes []gorm.ColumnType and execErr error
func (m Migrator) ColumnTypes(value interface{}) ([]gorm.ColumnType, error) {
    columnTypes := make([]gorm.ColumnType, 0)
    execErr := m.RunWithValue(value, func(stmt *gorm.Statement) (err error) {
        rows, err := m.DB.Session(&gorm.Session{}).Table(stmt.Table).Limit(1).Rows()
        if err != nil {
            return err
        }

        defer func() {
            err = rows.Close()
        }()

        var rawColumnTypes []*sql.ColumnType
        rawColumnTypes, err = rows.ColumnTypes()
        if err != nil {
            return err
        }

        for _, c := range rawColumnTypes {
            columnTypes = append(columnTypes, c)
        }
        return
    })

    return columnTypes, execErr
}

func (m Migrator) AddColumn(value interface{}, column string) error {
    if m.HasColumn(value, column) {
        return nil
    }
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        field, err := m.lookUpField(stmt, column)
        if err != nil {
            return fmt.Errorf("add column failed: %s", err)
        }
        dbName := field.DBName
        if IsReservedWord(dbName) {
            dbName = fmt.Sprintf(`"%s"`, dbName)
        }
        return m.DB.Exec(
            "ALTER TABLE ? ADD ? ?",
            clause.Table{Name: stmt.Table}, clause.Column{Name: dbName}, m.DB.Migrator().FullDataTypeOf(field),
        ).Error
    })
}

func (m Migrator) DropColumn(value interface{}, column string) error {
    if !m.HasColumn(value, column) {
        return nil
    }

    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        field, err := m.lookUpField(stmt, column)
        if err != nil {
            return fmt.Errorf("drop column failed: %s", err)
        }

        return m.DB.Exec(
            "ALTER TABLE ? DROP ?",
            clause.Table{Name: stmt.Table},
            clause.Column{Name: field.DBName},
        ).Error
    })
}

func (m Migrator) AlterColumn(value interface{}, column string) error {
    if !m.HasColumn(value, column) {
        return nil
    }

    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        field, err := m.lookUpField(stmt, column)
        if err != nil {
            return fmt.Errorf("alter column failed: %s", err)
        }
        dbName := field.DBName
        if IsReservedWord(dbName) {
            dbName = fmt.Sprintf(`"%s"`, dbName)
        }
        return m.DB.Exec(
            "ALTER TABLE ? MODIFY ? ?",
            clause.Table{Name: stmt.Table},
            clause.Column{Name: dbName},
            m.FullDataTypeOf(field),
        ).Error
    })
}

func (m Migrator) lookUpField(stmt *gorm.Statement, column string) (*schema.Field, error) {
    field := stmt.Schema.LookUpField(column)
    if field == nil {
        return nil, fmt.Errorf("field: %s of table: %s not found in stmt.Schema", column, stmt.Table)
    }
    return field, nil
}

func (m Migrator) HasColumn(value interface{}, column string) bool {
    var count int64
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        field := stmt.Schema.LookUpField(column)
        if field == nil {
            return nil
        }
        return m.DB.Raw("SELECT COUNT(1) FROM USER_TAB_COLUMNS WHERE TABLE_NAME = ? AND COLUMN_NAME = ?",
            strings.ToUpper(stmt.Table), strings.ToUpper(field.DBName),
        ).Row().Scan(&count)

    }) == nil && count > 0
}

func (m Migrator) CreateConstraint(value interface{}, name string) error {
    m.TryRemoveOnUpdate(value)
    return m.Migrator.CreateConstraint(value, name)
}

func (m Migrator) DropConstraint(value interface{}, name string) error {
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        for _, chk := range stmt.Schema.ParseCheckConstraints() {
            if chk.Name == name {
                return m.DB.Exec(
                    "ALTER TABLE ? DROP CHECK ?",
                    clause.Table{Name: stmt.Table}, clause.Column{Name: name},
                ).Error
            }
        }

        return m.DB.Exec(
            "ALTER TABLE ? DROP CONSTRAINT ?",
            clause.Table{Name: stmt.Table}, clause.Column{Name: name},
        ).Error
    })
}

func (m Migrator) HasConstraint(value interface{}, name string) bool {
    var count int64
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        return m.DB.Raw(
            "SELECT COUNT(1) FROM USER_CONSTRAINTS WHERE TABLE_NAME = ? AND CONSTRAINT_NAME = ?",
            strings.ToUpper(stmt.Table),
            strings.ToUpper(name),
        ).Row().Scan(&count)
    }) == nil && count > 0
}

func (m Migrator) DropIndex(value interface{}, name string) error {
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        if idx := stmt.Schema.LookIndex(name); idx != nil {
            name = idx.Name
        }

        return m.DB.Exec("DROP INDEX ?", clause.Column{Name: name}, clause.Table{Name: stmt.Table}).Error
    })
}

func (m Migrator) HasIndex(value interface{}, name string) bool {
    var count int64
    m.RunWithValue(value, func(stmt *gorm.Statement) error {
        if idx := stmt.Schema.LookIndex(name); idx != nil {
            name = idx.Name
        }
        return m.DB.Raw(
            "SELECT COUNT(1) FROM USER_INDEXES WHERE TABLE_NAME = ? AND INDEX_NAME = ?",
            strings.ToUpper(m.Migrator.DB.NamingStrategy.TableName(stmt.Table)),
            strings.ToUpper(name),
        ).Row().Scan(&count)
    })

    return count > 0
}

func (m Migrator) RenameIndex(value interface{}, oldName, newName string) error {
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        return m.DB.Exec(
            "ALTER INDEX ?.? RENAME TO ?", // wat
            clause.Table{Name: stmt.Table}, clause.Column{Name: oldName}, clause.Column{Name: newName},
        ).Error
    })
}

func (m Migrator) TryRemoveOnUpdate(value interface{}) error {
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        for _, rel := range stmt.Schema.Relationships.Relations {
            constraint := rel.ParseConstraint()
            if constraint != nil {
                rel.Field.TagSettings["CONSTRAINT"] = strings.ReplaceAll(rel.Field.TagSettings["CONSTRAINT"], fmt.Sprintf("ON UPDATE %s", constraint.OnUpdate), "")
            }
        }
        return nil
    })
}

func (m Migrator) TryQuotifyReservedWords(value interface{}) error {
    return m.RunWithValue(value, func(stmt *gorm.Statement) error {
        for idx, v := range stmt.Schema.DBNames {
            if IsReservedWord(v) {
                n := fmt.Sprintf(`"%s"`, v)
                stmt.Schema.DBNames[idx] = n
                stmt.Schema.FieldsByDBName[n] = stmt.Schema.FieldsByDBName[v]
                delete(stmt.Schema.FieldsByDBName, v)
            }
        }

        for _, v := range stmt.Schema.Fields {
            if IsReservedWord(v.DBName) {
                v.DBName = fmt.Sprintf(`"%s"`, v.DBName)
            }
        }
        tableName := stmt.Schema.Table
        if IsReservedWord(tableName) {
            stmt.Schema.Table = fmt.Sprintf(`"%s"`, tableName)
        }
        return nil
    })
}

func (m Migrator) dropSequenceIfExists(db *gorm.DB, sequenceName string) error {
    return db.Transaction(func(tx *gorm.DB) error {
        var count int
        if err := tx.Raw("SELECT COUNT(1) FROM USER_SEQUENCES WHERE SEQUENCE_NAME = ?", sequenceName).Row().Scan(&count); err != nil {
            return err
        }
        if count == 0 {
            return nil
        }
        dropSequenceIfExists := fmt.Sprintf("DROP SEQUENCE %s", sequenceName)
        if err := tx.Exec(dropSequenceIfExists).Error; err != nil {
            return err
        }
        return nil
    })
}

func (m Migrator) CreateSequence(values []interface{}) error {
    for _, value := range m.ReorderModels(values, false) {
        tx := m.DB.Session(&gorm.Session{})
        if err := m.RunWithValue(value, func(stmt *gorm.Statement) error {
            for _, v := range stmt.Schema.Fields {
                sequenceName := GenSequenceName(v.Schema.Table)
                if v.AutoIncrement {
                    if err := m.dropSequenceIfExists(tx, sequenceName); err != nil {
                        return err
                    }
                    createSequence := fmt.Sprintf("CREATE SEQUENCE %s START WITH 1 INCREMENT BY 1", sequenceName)
                    return tx.Exec(createSequence).Error
                }
            }
            return nil
        }); err != nil {
            return err
        }
    }
    return nil
}
