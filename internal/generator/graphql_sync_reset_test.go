package generator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mvanhorn/cli-printing-press/v4/internal/graphql"
	"github.com/mvanhorn/cli-printing-press/v4/internal/naming"
	"github.com/mvanhorn/cli-printing-press/v4/internal/spec"
	"github.com/stretchr/testify/require"
)

func TestGeneratedGraphQLSyncResetCommand(t *testing.T) {
	t.Parallel()
	apiSpec, err := graphql.ParseSDLBytes("resetstate.graphql", []byte(`
type Query {
  issues(first: Int, after: String): IssueConnection
}
type IssueConnection {
  nodes: [Issue]
  pageInfo: PageInfo
}
type PageInfo {
  hasNextPage: Boolean!
  endCursor: String
}
type Issue {
  id: ID!
  title: String!
}
`))
	require.NoError(t, err)
	apiSpec.Name = "resetstate"
	apiSpec.Auth = spec.AuthConfig{Type: "none"}
	outputDir := filepath.Join(t.TempDir(), naming.CLI(apiSpec.Name))
	require.NoError(t, New(apiSpec, outputDir).Generate())
	behaviorTest := `package cli

import (
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "resetstate-pp-cli/internal/store"
)

func TestSyncResetCommand(t *testing.T) {
 for _, tc := range []struct {
  name string
  args []string
  fail bool
  invalid bool
  bounded bool
  malformed bool
 }{
  {name:"invalid-since-full", args:[]string{"--full","--since","invalid"}, invalid:true},
  {name:"invalid-since-latest", args:[]string{"--latest-only","--since","invalid"}, invalid:true},
  {name:"invalid-resource-full", args:[]string{"--full","--resources","issues,unknown"}, invalid:true},
  {name:"invalid-page-limit-full", args:[]string{"--full","--max-pages","-1"}, invalid:true},
  {name:"failed-full", args:[]string{"--full"}, fail:true},
  {name:"failed-latest", args:[]string{"--latest-only"}, fail:true},
  {name:"completed-full", args:[]string{"--full"}},
  {name:"completed-latest", args:[]string{"--latest-only"}},
  {name:"bounded-max-pages", args:[]string{"--full","--max-pages","1"}, bounded:true},
  {name:"bounded-latest", args:[]string{"--latest-only"}, bounded:true},
  {name:"unproven-page-limit", args:[]string{"--full","--max-pages","1"}, malformed:true},
 } {
  t.Run(tc.name,func(t *testing.T) {
   path := filepath.Join(t.TempDir(),"data.db")
   db,err := store.Open(path)
   if err != nil { t.Fatal(err) }
   defer db.Close()
   watermark := time.Date(2020,1,1,0,0,0,0,time.UTC)
   if err := db.SaveSyncStateAt("issues","old-cursor",7,watermark); err != nil { t.Fatal(err) }
   beforeCursor,beforeTime,beforeCount,err := db.GetSyncState("issues")
   if err != nil { t.Fatal(err) }
   calls:=0
   server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request) {
    calls++
    // Check the checkpoint during the first HTTP request, before the
    // resource loop can hide a falsely completed reset behind a final write.
    cursor,stamp,count,err:=db.GetSyncState("issues")
    var complete int
    markerErr:=db.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type='issues'").Scan(&complete)
    if err!=nil || markerErr!=nil || cursor!="" || !stamp.Equal(watermark) || count!=7 || complete!=0 {
     t.Errorf("in-flight checkpoint = %q %s %d complete=%d errors=%v/%v",cursor,stamp,count,complete,err,markerErr)
    }
    var req struct { Variables map[string]any }
    if err:=json.NewDecoder(r.Body).Decode(&req);err!=nil {t.Error(err)}
    if after,_:=req.Variables["after"].(string);after!="" {t.Errorf("reset resumed cursor %q",after)}
    if tc.fail { http.Error(w,"request refused",http.StatusBadRequest);return }
    w.Header().Set("Content-Type","application/json")
    if tc.bounded || tc.malformed {
     cursor:="next-page"
     if tc.malformed {cursor=""}
     json.NewEncoder(w).Encode(map[string]any{"data":map[string]any{"issues":map[string]any{"nodes":[]any{map[string]any{"id":"one","title":"One"}},"pageInfo":map[string]any{"hasNextPage":true,"endCursor":cursor}}}})
     return
    }
    io.WriteString(w,"{\"data\":{\"issues\":{\"nodes\":[{\"id\":\"one\",\"title\":\"One\"}],\"pageInfo\":{\"hasNextPage\":false,\"endCursor\":\"end\"}}}}")
   }))
   defer server.Close()
   t.Setenv("RESETSTATE_BASE_URL",server.URL)
   cmd:=newSyncCmd(&rootFlags{configPath:filepath.Join(t.TempDir(),"config.yaml"),timeout:time.Second})
   cmd.SetOut(io.Discard)
   cmd.SetErr(io.Discard)
   cmd.SetArgs(append([]string{"--db",path,"--resources","issues"},tc.args...))
   events,openErr:=os.CreateTemp(t.TempDir(),"events")
   if openErr!=nil {t.Fatal(openErr)}
   defer events.Close()
   oldStderr,oldHuman:=os.Stderr,humanFriendly
   os.Stderr,humanFriendly=events,false
   func(){defer func(){os.Stderr,humanFriendly=oldStderr,oldHuman}();err=cmd.Execute()}()
   output,readErr:=os.ReadFile(events.Name())
   if readErr!=nil {t.Fatal(readErr)}
   if (tc.fail || tc.invalid || tc.malformed) != (err!=nil) {t.Fatalf("command error=%v output=%s",err,output)}
   cursor,stamp,count,stateErr:=db.GetSyncState("issues")
   if stateErr!=nil {t.Fatal(stateErr)}
   var complete int
   if err:=db.DB().QueryRow("SELECT last_attempt_complete FROM sync_state WHERE resource_type='issues'").Scan(&complete);err!=nil {t.Fatal(err)}
   if tc.invalid {
    if calls!=0 || cursor!=beforeCursor || !stamp.Equal(beforeTime) || count!=beforeCount || complete!=1 {
     t.Fatalf("invalid input mutated checkpoint: calls=%d %q %s count=%d complete=%d",calls,cursor,stamp,count,complete)
    }
    if strings.Contains(tc.name,"since") && !strings.Contains(err.Error(),"invalid --since") { t.Fatalf("unexpected validation error: %v",err) }
    return
   }
   if calls!=1 {t.Fatalf("calls=%d, want1",calls)}
   if tc.bounded {
    expectedCursor:="next-page"
    if cursor!=expectedCursor || !stamp.Equal(watermark) || count!=1 || complete!=0 {
     t.Fatalf("bounded checkpoint=%q %s count=%d complete=%d",cursor,stamp,count,complete)
    }
    text:=string(output)
    if !strings.Contains(text,"\"event\":\"sync_partial\"") || !strings.Contains(text,"\"total_records\":1") || !strings.Contains(text,"\"success\":1,\"warned\":0,\"errored\":0") || strings.Contains(text,"\"event\":\"sync_complete\"") || strings.Contains(text,"insufficient access") {t.Fatalf("bounded output=%s",output)}
   } else if tc.malformed {
    if !stamp.Equal(watermark) || complete!=0 || strings.Contains(err.Error(),"insufficient access") {t.Fatalf("unproven pagination checkpoint=%s complete=%d error=%v",stamp,complete,err)}
   } else if tc.fail {
    if cursor!="" || !stamp.Equal(watermark) || count!=7 || complete!=0 {
     t.Fatalf("failed reset checkpoint=%q %s count=%d complete=%d",cursor,stamp,count,complete)
    }
   } else if cursor!="" || !stamp.After(watermark) || count!=1 || complete!=1 {
    t.Fatalf("natural completion checkpoint=%q %s count=%d complete=%d",cursor,stamp,count,complete)
   }
  })
 }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(outputDir, "internal", "cli", "sync_reset_command_test.go"), []byte(behaviorTest), 0o644))
	runGoCommandRequired(t, outputDir, "test", "./internal/cli", "-run", "^TestSyncResetCommand$")
	requireGeneratedCompiles(t, outputDir)
}
