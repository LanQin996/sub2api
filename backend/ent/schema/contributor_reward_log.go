package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ContributorRewardLog records idempotent realtime rewards paid to account contributors.
type ContributorRewardLog struct {
	ent.Schema
}

func (ContributorRewardLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "contributor_reward_logs"}}
}

func (ContributorRewardLog) Fields() []ent.Field {
	return []ent.Field{
		field.String("request_id").MaxLen(128).NotEmpty(),
		field.Int64("api_key_id"),
		field.Int64("owner_user_id"),
		field.Int64("consumer_user_id"),
		field.Int64("account_id"),
		field.Int64("group_id").Optional().Nillable(),
		field.Float("total_cost").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("actual_cost").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Float("reward_multiplier").SchemaType(map[string]string{dialect.Postgres: "decimal(10,4)"}).Default(0),
		field.Float("reward_amount").SchemaType(map[string]string{dialect.Postgres: "decimal(20,10)"}).Default(0),
		field.Time("created_at").Default(time.Now).Immutable().SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ContributorRewardLog) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("contributor_reward_logs").Field("owner_user_id").Required().Unique(),
		edge.From("account", Account.Type).Ref("contributor_reward_logs").Field("account_id").Required().Unique(),
	}
}

func (ContributorRewardLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("request_id", "api_key_id").Unique(),
		index.Fields("owner_user_id", "created_at"),
		index.Fields("account_id", "created_at"),
		index.Fields("group_id", "created_at"),
	}
}
