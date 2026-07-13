package clauses

import (
	"gorm.io/gorm/clause"
)

const VectorSearchName = "VECTOR_SEARCH"

// VectorSearch 向量相似度检索子句，由 yashandb-gorm 方言改写 ORDER BY / LIMIT。
type VectorSearch struct {
	Column         clause.Column
	QueryVector    interface{}
	DistanceMetric string
	Approximate    bool
	WithScore      bool
	ScoreAlias     string
	TopK           int
}

func (vs VectorSearch) Name() string {
	return VectorSearchName
}

func (vs VectorSearch) Build(clause.Builder) {}

func (vs VectorSearch) MergeClause(c *clause.Clause) {
	c.Name = vs.Name()
	c.Expression = vs
}
