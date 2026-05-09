package graphql

import (
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSDL = `
type Query {
  issues(first: Int, after: String, filter: IssueFilter): IssueConnection!
  issue(id: String!): Issue!
  teams: TeamConnection!
  viewer: User!
}

type Mutation {
  issueCreate(input: IssueCreateInput!): IssuePayload!
  issueUpdate(id: String!, input: IssueUpdateInput!): IssuePayload!
  issueArchive(id: String!): IssueArchivePayload!
}

type Issue {
  id: ID!
  identifier: String!
  title: String!
  description: String
  priority: Int!
  state: WorkflowState!
  assignee: User
  team: Team!
  createdAt: DateTime!
  updatedAt: DateTime!
}

type IssueConnection {
  nodes: [Issue!]!
  pageInfo: PageInfo!
}

type Team {
  id: ID!
  name: String!
  key: String!
}

type TeamConnection {
  nodes: [Team!]!
  pageInfo: PageInfo!
}

type User {
  id: ID!
  name: String!
  email: String
}

type WorkflowState {
  id: ID!
  name: String!
  type: String!
}

type PageInfo {
  hasNextPage: Boolean!
  endCursor: String
}

input IssueCreateInput {
  title: String!
  description: String
  teamId: String!
  priority: Int
  assigneeId: String
}

input IssueUpdateInput {
  title: String
  description: String
  priority: Int
  assigneeId: String
  stateId: String
}

type IssuePayload {
  issue: Issue
}

type IssueArchivePayload {
  entity: Issue
}

scalar DateTime
`

func TestParseSDLContent(t *testing.T) {
	parsed, err := ParseSDLBytes("linear-schema.graphql", []byte(testSDL))
	require.NoError(t, err)

	assert.Equal(t, "linear", parsed.Name)
	assert.Equal(t, "https://api.linear.app", parsed.BaseURL)
	assert.Equal(t, "/graphql", parsed.GraphQLEndpointPath)
	assert.Empty(t, parsed.EndpointTemplateVars)
	assert.Equal(t, "api_key", parsed.Auth.Type)
	assert.Equal(t, []string{"LINEAR_API_KEY"}, parsed.Auth.EnvVars)

	issues := parsed.Resources["issues"]
	require.NotNil(t, issues.Endpoints)

	list := issues.Endpoints["list"]
	assert.Equal(t, "GET", list.Method)
	assert.Equal(t, "/graphql", list.Path)
	require.NotNil(t, list.Pagination)
	assert.Equal(t, "cursor", list.Pagination.Type)
	assert.Equal(t, "first", list.Pagination.LimitParam)
	assert.Equal(t, "after", list.Pagination.CursorParam)
	assert.Equal(t, "data.issues.nodes", list.ResponsePath)
	assert.Equal(t, "Issue", list.Response.Item)

	get := issues.Endpoints["get"]
	assert.Equal(t, "GET", get.Method)
	require.Len(t, get.Params, 1)
	assert.Equal(t, "id", get.Params[0].Name)
	assert.True(t, get.Params[0].Required)
	assert.True(t, get.Params[0].Positional)

	create := issues.Endpoints["create"]
	assert.Equal(t, "POST", create.Method)
	assert.ElementsMatch(t, []string{"title", "description", "teamId", "priority", "assigneeId"}, paramNames(create.Body))
	assert.True(t, bodyParam(create.Body, "title").Required)
	assert.True(t, bodyParam(create.Body, "teamId").Required)

	update := issues.Endpoints["update"]
	assert.Equal(t, "PATCH", update.Method)
	require.Len(t, update.Params, 1)
	assert.Equal(t, "id", update.Params[0].Name)
	assert.True(t, update.Params[0].Positional)
	assert.ElementsMatch(t, []string{"title", "description", "priority", "assigneeId", "stateId"}, paramNames(update.Body))

	del := issues.Endpoints["delete"]
	assert.Equal(t, "DELETE", del.Method)
	require.Len(t, del.Params, 1)
	assert.Equal(t, "id", del.Params[0].Name)
	assert.True(t, del.Params[0].Positional)

	_, hasIssues := parsed.Resources["issues"]
	_, hasTeams := parsed.Resources["teams"]
	_, hasUsers := parsed.Resources["users"]
	_, hasIssueConnection := parsed.Resources["issue-connections"]
	_, hasPageInfo := parsed.Resources["page-infos"]
	assert.True(t, hasIssues)
	assert.True(t, hasTeams)
	assert.True(t, hasUsers)
	assert.False(t, hasIssueConnection)
	assert.False(t, hasPageInfo)

	assert.Contains(t, parsed.Types, "Issue")
	assert.Contains(t, parsed.Types, "Team")
	assert.Contains(t, parsed.Types, "User")
	assert.Contains(t, parsed.Types, "WorkflowState")
	assert.NotContains(t, parsed.Types, "IssueConnection")
	assert.NotContains(t, parsed.Types, "PageInfo")
	assert.NotContains(t, parsed.Types, "IssueCreateInput")
	assert.NotContains(t, parsed.Types, "IssuePayload")
}

func TestBuildTypeDefDeduplicatesFields(t *testing.T) {
	// Schema where a type has duplicate field names (e.g., pagination args
	// mixed in with entity fields, as happens in large GraphQL schemas like Linear's).
	sdl := `
type Query {
  things(first: Int, after: String): ThingConnection!
}

type ThingConnection {
  nodes: [Thing!]!
  pageInfo: PageInfo!
}

type PageInfo {
  hasNextPage: Boolean!
  endCursor: String
}

type Thing {
  id: ID!
  name: String!
  after: String
  before: String
  first: Int
  after: String
  before: String
}
`
	parsed, err := ParseSDLBytes("test-dedup.graphql", []byte(sdl))
	require.NoError(t, err)

	thingType, ok := parsed.Types["Thing"]
	require.True(t, ok, "Thing type should be present")

	// Verify no duplicate field names
	seen := map[string]int{}
	for _, field := range thingType.Fields {
		seen[field.Name]++
	}
	for name, count := range seen {
		assert.Equal(t, 1, count, "field %q appears %d times, expected 1", name, count)
	}

	// Verify all unique fields are present
	fieldNames := make([]string, 0, len(thingType.Fields))
	for _, f := range thingType.Fields {
		fieldNames = append(fieldNames, f.Name)
	}
	assert.Contains(t, fieldNames, "id")
	assert.Contains(t, fieldNames, "name")
	assert.Contains(t, fieldNames, "after")
	assert.Contains(t, fieldNames, "before")
	assert.Contains(t, fieldNames, "first")
}

func TestParseSDLMondayFlatSnakeCase(t *testing.T) {
	// Monday-style flat snake_case mutations (create_board, move_item_to_group, etc.)
	// must cluster into resources the same way PascalCase xxxCreate/xxxUpdate do.
	// Before the fix this returns 0 resources because classifyMutation only recognises
	// PascalCase verbs (create/update/delete substring match) but not verb-prefix style.
	sdl := `
schema {
  query: Query
  mutation: Mutation
}

type Query {
  boards(ids: [ID!], limit: Int = 25): [Board]
  items(ids: [ID!], limit: Int = 25): [Item]
  updates(ids: [ID!], limit: Int = 25): [Update]
}

type Mutation {
  create_board(board_name: String!, board_kind: BoardKind!): Board
  archive_board(board_id: ID!): Board
  delete_board(board_id: ID!): Board
  create_item(board_id: ID!, item_name: String!): Item
  delete_item(item_id: ID): Item
  move_item_to_group(item_id: ID, group_id: String!): Item
  change_column_value(board_id: ID!, item_id: ID, column_id: String!, value: JSON!): Item
  create_update(item_id: ID, body: String!): Update
  delete_update(id: ID!): Update
}

type Board {
  id: ID!
  name: String!
  description: String
}

type Item {
  id: ID!
  name: String!
  board: Board
}

type Update {
  id: ID!
  body: String
  item_id: ID
}

enum BoardKind {
  public
  private
}

scalar JSON
`

	parsed, err := ParseSDLBytes("monday-schema.graphql", []byte(sdl))
	require.NoError(t, err)

	// Must produce at least 3 resources: boards, items, updates
	require.GreaterOrEqual(t, len(parsed.Resources), 3,
		"flat snake_case mutations must cluster into ≥3 resources; got %d: %v",
		len(parsed.Resources), resourceKeys(parsed.Resources))

	boards := parsed.Resources["boards"]
	require.NotNil(t, boards.Endpoints, "boards resource must have endpoints")
	assert.Contains(t, boards.Endpoints, "create", "boards must have create endpoint from create_board")
	assert.Contains(t, boards.Endpoints, "delete", "boards must have delete endpoint from delete_board/archive_board")

	items := parsed.Resources["items"]
	require.NotNil(t, items.Endpoints, "items resource must have endpoints")
	assert.Contains(t, items.Endpoints, "create", "items must have create endpoint from create_item")
	assert.Contains(t, items.Endpoints, "delete", "items must have delete endpoint from delete_item")

	updates := parsed.Resources["updates"]
	require.NotNil(t, updates.Endpoints, "updates resource must have endpoints")
	assert.Contains(t, updates.Endpoints, "create", "updates must have create endpoint from create_update")
	assert.Contains(t, updates.Endpoints, "delete", "updates must have delete endpoint from delete_update")
}

func resourceKeys(m map[string]spec.Resource) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func paramNames(params []spec.Param) []string {
	names := make([]string, 0, len(params))
	for _, param := range params {
		names = append(names, param.Name)
	}
	return names
}

func bodyParam(params []spec.Param, name string) spec.Param {
	for _, param := range params {
		if param.Name == name {
			return param
		}
	}
	return spec.Param{}
}
