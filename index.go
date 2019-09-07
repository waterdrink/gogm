package gogm

import (
	"errors"
	"fmt"
	"github.com/cornelk/hashmap"
	dsl "github.com/mindstand/go-cypherdsl"
	"github.com/mindstand/gogm/util"
)

//drops all known indexes
func dropAllIndexesAndConstraints() error{
	constraintRows, err := dsl.QB(true).Cypher("CALL db.constraints").Query(nil)
	if err != nil{
		return err
	}

	constraints, err := dsl.RowsToStringArray(constraintRows)
	if err != nil{
		return err
	}

	dropSess := dsl.NewSession()
	defer dropSess.Close()

	//if there is anything, get rid of it
	if len(constraints) != 0{
		err = dropSess.Begin(false)
		if err != nil{
			return err
		}

		for _, constraint := range constraints {
			log.Debugf("dropping constraint '%s'", constraint)
			_, err := dropSess.Query().Cypher(fmt.Sprintf("DROP %s", constraint)).Exec(nil)
			if err != nil{
				oerr := err
				err = dropSess.Rollback()
				if err != nil{
					return fmt.Errorf("failed to rollback, original error was %s", oerr.Error())
				}

				return oerr
			}
		}

		err = dropSess.Commit()
		if err != nil{
			return err
		}
	}

	indexRows, err := dsl.QB(true).Cypher("CALL db.indexes()").Query(nil)
	if err != nil{
		return err
	}

	indexes, err := dsl.RowsTo2DInterfaceArray(indexRows)
	if err != nil{
		return err
	}

	//if there is anything, get rid of it
	if len(indexes) != 0{
		err = dropSess.Begin(false)
		if err != nil{
			return err
		}

		for _, index := range indexes{
			if len(index) == 0{
				return errors.New("invalid index config")
			}

			_, err := dropSess.Query().Cypher(fmt.Sprintf("DROP %s", index[0].(string))).Exec(nil)
			if err != nil{
				oerr := err
				err = dropSess.Rollback()
				if err != nil{
					return fmt.Errorf("failed to rollback, original error was %s", oerr.Error())
				}

				return oerr
			}
		}

		return dropSess.Commit()
	} else {
		return nil
	}
}

//creates all indexes
func createAllIndexesAndConstraints(mappedTypes *hashmap.HashMap) error{
	//validate that we have to do anything
	if mappedTypes == nil || mappedTypes.Len() == 0{
		return errors.New("must have types to map")
	}

	numIndexCreated := 0

	//setup session
	sess := dsl.NewSession()
	defer sess.Close()

	err := sess.Begin(false)
	if err != nil{
		return err
	}

	//index and/or create unique constraints wherever necessary
	//for node, structConfig := range mappedTypes{
	for nodes := range mappedTypes.Iter(){
		node := nodes.Key.(string)
		structConfig := nodes.Value.(structDecoratorConfig)
		if structConfig.Fields == nil || len(structConfig.Fields) == 0{
			continue
		}

		var indexFields []string

		for _, config := range structConfig.Fields{
			//pk is a special unique key
			if config.PrimaryKey || config.Unique{
				numIndexCreated++

				_, err := sess.Query().Create(dsl.NewConstraint(&dsl.ConstraintConfig{
					Unique: true,
					Name: node,
					Type: structConfig.Label,
					Field: config.Name,
				})).Exec(nil)
				if err != nil{
					oerr := err
					err = sess.Rollback()
					if err != nil{
						return fmt.Errorf("failed to rollback, original error was %s", oerr.Error())
					}

					return oerr
				}
			} else if config.Index{
				indexFields = append(indexFields, config.Name)
			}
		}

		//create composite index
		if len(indexFields) > 0{
			numIndexCreated++
			_, err := sess.Query().Create(dsl.NewIndex(&dsl.IndexConfig{
				Type: structConfig.Label,
				Fields: indexFields,
			})).Exec(nil)
			if err != nil{
				oerr := err
				err = sess.Rollback()
				if err != nil{
					return fmt.Errorf("failed to rollback, original error was %s", oerr.Error())
				}

				return oerr
			}
		}
	}

	log.Debugf("created (%v) indexes", numIndexCreated)

	return sess.Commit()
}

//verifies all indexes
func verifyAllIndexesAndConstraints(mappedTypes *hashmap.HashMap) error{
	//validate that we have to do anything
	if mappedTypes == nil || mappedTypes.Len() == 0{
		return errors.New("must have types to map")
	}

	var constraints []string
	var indexes []string

	//build constraint strings
	for nodes := range mappedTypes.Iter(){
		node := nodes.Key.(string)
		structConfig := nodes.Value.(structDecoratorConfig)

		if structConfig.Fields == nil || len(structConfig.Fields) == 0{
			continue
		}

		fields := []string{}

		for _, config := range structConfig.Fields{

			if config.PrimaryKey || config.Unique{
				t := fmt.Sprintf("CONSTRAINT ON (%s:%s) ASSERT %s.%s IS UNIQUE", node, structConfig.Label, node, config.Name)
				constraints = append(constraints, t)

				indexes = append(indexes, fmt.Sprintf("INDEX ON :%s(%s)", structConfig.Label, config.Name))

			} else if config.Index{
				fields = append(fields, config.Name)
			}
		}

		f := "("
		for _, field := range fields{
			f += field
		}

		f += ")"

		indexes = append(indexes, fmt.Sprintf("INDEX ON :%s%s", structConfig.Label, f))

	}

	//get whats there now
	constRows, err := dsl.QB(true).Cypher("CALL db.constraints").Query(nil)
	if err != nil{
		return err
	}

	foundConstraints, err := dsl.RowsToStringArray(constRows)
	if err != nil{
		return err
	}

	var foundIndexes []string

	indexRows, err := dsl.QB(true).Cypher("CALL db.indexes()").Query(nil)
	if err != nil{
		return err
	}

	findexes, err := dsl.RowsTo2DInterfaceArray(indexRows)
	if err != nil{
		return err
	}

	if len(findexes) != 0{
		for _, index := range findexes{
			if len(index) == 0{
				return errors.New("invalid index config")
			}

			foundIndexes = append(foundIndexes, index[0].(string))
		}
	}

	//verify from there
	delta, found := util.Difference(foundIndexes, indexes)
	if !found{
		return fmt.Errorf("found differences in remote vs ogm for found indexes, %v", delta)
	}

	log.Debug(delta)

	delta, found = util.Difference(foundConstraints, constraints)
	if !found{
		return fmt.Errorf("found differences in remote vs ogm for found constraints, %v", delta)
	}

	log.Debug(delta)

	return nil
}
