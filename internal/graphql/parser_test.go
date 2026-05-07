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
