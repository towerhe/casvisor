// Copyright 2023 The casbin Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package object

import (
	"database/sql"
	"fmt"
	"runtime"
	"strings"

	"github.com/casbin/casvisor/util"

	"github.com/beego/beego"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"xorm.io/core"
	"xorm.io/xorm"
)

var adapter *Adapter

func InitConfig() {
	err := beego.LoadAppConfig("ini", "../conf/app.conf")
	if err != nil {
		panic(err)
	}

	InitAdapter()
	CreateTables()
}

func InitAdapter() {
	adapter = NewAdapter(beego.AppConfig.String("driverName"), beego.AppConfig.String("dataSourceName"))

	tableNamePrefix := beego.AppConfig.String("tableNamePrefix")
	tbMapper := core.NewPrefixMapper(core.SnakeMapper{}, tableNamePrefix)
	adapter.engine.SetTableMapper(tbMapper)
}

func CreateTables() {
	err := adapter.createDatabase()
	if err != nil {
		panic(err)
	}

	adapter.createTable()
}

// Adapter represents the MySQL adapter for policy storage.
type Adapter struct {
	driverName     string
	dataSourceName string
	engine         *xorm.Engine
}

// finalizer is the destructor for Adapter.
func finalizer(a *Adapter) {
	err := a.engine.Close()
	if err != nil {
		panic(err)
	}
}

// NewAdapter is the constructor for Adapter.
func NewAdapter(driverName string, dataSourceName string) *Adapter {
	a := &Adapter{}
	a.driverName = driverName
	a.dataSourceName = dataSourceName

	// Open the DB, create it if not existed.
	a.open()

	// Call the destructor when the object is released.
	runtime.SetFinalizer(a, finalizer)

	return a
}

func (a *Adapter) createDatabase() error {
	dbName := beego.AppConfig.String("dbName")

	switch a.driverName {
	case "mysql":
		return a.createDatabaseForMySQL(dbName)
	case "postgres":
		return a.createDatabaseForPostgres(dbName)
	default:
		return nil
	}
}

func (a *Adapter) createDatabaseForMySQL(dbName string) error {
	dsn := a.dataSourceName + "mysql"
	engine, err := xorm.NewEngine(a.driverName, dsn)
	if err != nil {
		return err
	}
	defer engine.Close()

	_, err = engine.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s default charset utf8 COLLATE utf8_general_ci", dbName))

	return err
}

func (a *Adapter) createDatabaseForPostgres(dbName string) error {
	dsn := strings.ReplaceAll(a.dataSourceName, dbName, "postgres")
	engine, err := xorm.NewEngine(a.driverName, dsn)
	if err != nil {
		return err
	}
	defer engine.Close()

	rows, err := engine.DB().Query(fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = '%s'", dbName))
	if err != nil {
		return err
	}
	defer rows.Close()

	if !rows.Next() {
		if _, err = engine.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName)); err != nil {
			return err
		}
	}

	schema := util.GetParamFromDataSourceName(a.dataSourceName, "search_path")
	if schema != "" {
		db, err := sql.Open(a.driverName, a.dataSourceName)
		if err != nil {
			return err
		}
		defer db.Close()

		_, err = db.Exec(fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s;", schema))
		if err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				return err
			}
		}
	}

	return nil
}

func (a *Adapter) open() {
	dsn := a.dataSourceName
	if a.driverName == "mysql" {
		dsn = a.dataSourceName + beego.AppConfig.String("dbName")
	}

	engine, err := xorm.NewEngine(a.driverName, dsn)
	if err != nil {
		panic(err)
	}
	if a.driverName == "postgres" {
		schema := util.GetParamFromDataSourceName(a.dataSourceName, "search_path")
		if schema != "" {
			engine.SetSchema(schema)
		}
	}
	a.engine = engine
}

func (a *Adapter) close() {
	a.engine.Close()
	a.engine = nil
}

func (a *Adapter) createTable() {
	err := a.engine.Sync2(new(Dataset))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Record))
	if err != nil {
		panic(err)
	}

	err = a.engine.Sync2(new(Asset))
	if err != nil {
		panic(err)
	}
}

func GetSession(owner string, offset, limit int, field, value, sortField, sortOrder string) *xorm.Session {
	session := adapter.engine.Prepare()
	if offset != -1 && limit != -1 {
		session.Limit(limit, offset)
	}
	if owner != "" {
		session = session.And("owner=?", owner)
	}
	if field != "" && value != "" {
		if util.FilterField(field) {
			session = session.And(fmt.Sprintf("%s like ?", util.SnakeString(field)), fmt.Sprintf("%%%s%%", value))
		}
	}
	if sortField == "" || sortOrder == "" {
		sortField = "created_time"
	}
	if sortOrder == "ascend" {
		session = session.Asc(util.SnakeString(sortField))
	} else {
		session = session.Desc(util.SnakeString(sortField))
	}
	return session
}
