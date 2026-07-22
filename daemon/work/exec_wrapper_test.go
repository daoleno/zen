package work

import (
	"strings"
	"testing"
)

func TestParseCodexExecWrapper_SingleExecCommand(t *testing.T) {
	input := `const r = await tools.exec_command({"cmd":"rg -n foo app","workdir":"/tmp","yield_time_ms":10000});
text(r.output);`
	calls := parseCodexExecWrapper(input)
	if len(calls) != 1 {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0].Name != "exec_command" {
		t.Fatalf("name = %q", calls[0].Name)
	}
	if got := nestedCallCommand(calls[0]); got != "rg -n foo app" {
		t.Fatalf("command = %q", got)
	}
}

func TestParseCodexExecWrapper_ApplyPatchViaConst(t *testing.T) {
	input := "const patch = \"*** Begin Patch\\n*** Update File: app/services/storage.ts\\n@@\\n-old\\n+new\\n*** End Patch\";\ntext(await tools.apply_patch(patch));"
	calls := parseCodexExecWrapper(input)
	if len(calls) != 1 || calls[0].Name != "apply_patch" {
		t.Fatalf("calls = %#v", calls)
	}
	patch := nestedCallPatchText(calls[0])
	if !strings.Contains(patch, "*** Update File: app/services/storage.ts") {
		t.Fatalf("patch = %q", patch)
	}
	files := patchSurfaces(patch)
	if len(files) != 1 || files[0] != "app/services/storage.ts" {
		t.Fatalf("files = %#v", files)
	}
}

func TestPatchFileChanges_LeavesUnreportedDeleteStatsUnknown(t *testing.T) {
	changes := patchFileChanges("*** Begin Patch\n*** Delete File: src/legacy.ts\n*** End Patch")
	if len(changes) != 1 {
		t.Fatalf("changes = %#v", changes)
	}
	change := changes[0]
	if change.Path != "src/legacy.ts" || change.Operation != "delete" {
		t.Fatalf("change = %#v", change)
	}
	if change.Additions != nil || change.Deletions != nil {
		t.Fatalf("delete stats should be unknown, got %#v", change)
	}
}

func TestPatchFileChanges_DoesNotTreatPatchContextAsMetadata(t *testing.T) {
	changes := patchFileChanges("*** Begin Patch\n*** Update File: src/parser.ts\n@@\n *** Update File: synthetic content\n-old\n+new\n*** End Patch")
	if len(changes) != 1 || changes[0].Path != "src/parser.ts" {
		t.Fatalf("changes = %#v, want one real target", changes)
	}
	change := changes[0]
	if change.Additions == nil || *change.Additions != 1 || change.Deletions == nil || *change.Deletions != 1 {
		t.Fatalf("change stats = %#v, want +1 -1", change)
	}
}

func TestParseCodexExecWrapper_UpdatePlan(t *testing.T) {
	input := `const p = await tools.update_plan({plan:[
  {step:"Trace pipeline",status:"in_progress"},
  {step:"Add tests",status:"pending"}
]});
text(p);`
	calls := parseCodexExecWrapper(input)
	if len(calls) != 1 || calls[0].Name != "update_plan" {
		t.Fatalf("calls = %#v", calls)
	}
	_, plan := nestedCallPlan(calls[0])
	if len(plan) != 2 {
		t.Fatalf("plan = %#v", plan)
	}
	if plan[0].Step != "Trace pipeline" || plan[0].Status != "in_progress" {
		t.Fatalf("step0 = %#v", plan[0])
	}
	if plan[1].Status != "pending" {
		t.Fatalf("step1 = %#v", plan[1])
	}
}

func TestParseCodexExecWrapper_ViewImageAndMultiple(t *testing.T) {
	single := `const r = await tools.view_image({path:"/tmp/fixture.png", detail:"original"});
image(r.image_url);`
	calls := parseCodexExecWrapper(single)
	if len(calls) != 1 || calls[0].Name != "view_image" {
		t.Fatalf("single = %#v", calls)
	}
	if nestedCallViewPath(calls[0]) != "/tmp/fixture.png" {
		t.Fatalf("path = %#v", calls[0])
	}

	multi := `const results = await Promise.all([
  tools.exec_command({"cmd":"go test ./work -count=1","workdir":"/repo"}),
  tools.exec_command({"cmd":"bunx tsc --noEmit","workdir":"/repo/app"})
]);`
	calls = parseCodexExecWrapper(multi)
	if len(calls) != 2 {
		t.Fatalf("multi = %#v", calls)
	}
}

func TestParseCodexExecWrapper_EscapedAndMalformed(t *testing.T) {
	escaped := `const r = await tools.exec_command({"cmd":"echo \"hello\" && rg -n 'token'"});`
	calls := parseCodexExecWrapper(escaped)
	if len(calls) != 1 {
		t.Fatalf("escaped calls = %#v", calls)
	}
	if !strings.Contains(nestedCallCommand(calls[0]), `echo "hello"`) {
		t.Fatalf("escaped command = %q", nestedCallCommand(calls[0]))
	}

	malformed := `const r = await tools.exec_command({"cmd":"unterminated`
	if got := parseCodexExecWrapper(malformed); len(got) != 0 {
		t.Fatalf("malformed should yield no calls, got %#v", got)
	}
}

func TestParseCodexExecWrapper_IgnoresToolLikeTextInNonCode(t *testing.T) {
	input := "const r = await tools.exec_command({\"cmd\":\"rg -n tools.apply_patch( app\"});\n" +
		"// tools.view_image({path:\"/tmp/x.png\"})\n" +
		"/* tools.update_plan({plan:[]}) */\n" +
		"const note = `look at tools.exec_command({\"cmd\":\"echo hi\"})`;\n" +
		"text(r.output);\n"
	calls := parseCodexExecWrapper(input)
	if len(calls) != 1 || calls[0].Name != "exec_command" {
		t.Fatalf("calls = %#v, want only real exec_command", calls)
	}
	cmd := nestedCallCommand(calls[0])
	if !strings.Contains(cmd, "tools.apply_patch") {
		t.Fatalf("command should keep tool-like text inside JSON string, got %q", cmd)
	}
}

func TestParseCodexExecWrapper_PreservesPromiseAll(t *testing.T) {
	input := `const results = await Promise.all([
  tools.exec_command({"cmd":"go test ./work -count=1"}),
  tools.view_image({path:"/tmp/fixture.png"})
]);`
	calls := parseCodexExecWrapper(input)
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	if calls[0].Name != "exec_command" || calls[1].Name != "view_image" {
		t.Fatalf("names = %#v", calls)
	}
}

func TestParseCodexConversation_ExecWrapperPromotesNestedActions(t *testing.T) {
	path := t.TempDir() + "/rollout.jsonl"
	writeJSONL(t, path,
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:00Z",
			"payload": map[string]any{
				"type":    "custom_tool_call",
				"name":    "exec",
				"call_id": "call-search",
				"input":   "const r = await tools.exec_command({\"cmd\":\"rg -n SemanticAction app\",\"workdir\":\"/repo\"});\ntext(r.output);",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:01Z",
			"payload": map[string]any{
				"type":    "custom_tool_call_output",
				"call_id": "call-search",
				"output":  "Chunk ID: x\nWall time: 0.1 seconds\nProcess exited with code 0\nOutput:\napp/services/toolCallSemantics.ts:1:export",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:02Z",
			"payload": map[string]any{
				"type":    "custom_tool_call",
				"name":    "exec",
				"call_id": "call-plan",
				"input":   "const p = await tools.update_plan({plan:[{step:\"Implement\",status:\"in_progress\"},{step:\"Verify\",status:\"pending\"}]});\ntext(p);",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:03Z",
			"payload": map[string]any{
				"type":    "custom_tool_call",
				"name":    "exec",
				"call_id": "call-multi",
				"input":   "await Promise.all([tools.exec_command({\"cmd\":\"go test ./work -count=1\"}), tools.view_image({path:\"/tmp/fixture.png\"})]);",
			},
		},
		map[string]any{
			"type":      "response_item",
			"timestamp": "2026-05-20T10:00:04Z",
			"payload": map[string]any{
				"type":      "function_call",
				"name":      "view_image",
				"call_id":   "call-legacy",
				"arguments": `{"path":"/tmp/legacy.png"}`,
			},
		},
	)

	got, err := parseCodexConversation(path)
	if err != nil {
		t.Fatalf("parseCodexConversation: %v", err)
	}
	if len(got.Events) < 4 {
		t.Fatalf("events = %#v", got.Events)
	}

	command := got.Events[0]
	if command.Kind != "command" || command.Command != "rg -n SemanticAction app" || command.ToolName == "exec" {
		t.Fatalf("command event = %#v", command)
	}
	if command.Status != "done" || !strings.Contains(command.Body, "toolCallSemantics.ts") {
		t.Fatalf("command completion = %#v", command)
	}

	plan := got.Events[1]
	if plan.Kind != "plan" || len(plan.Plan) != 2 {
		t.Fatalf("plan event = %#v", plan)
	}

	multi := got.Events[2]
	if multi.Kind != "tool" || !strings.HasPrefix(multi.ToolName, "multi:") || strings.Contains(multi.ToolName, "exec,") {
		t.Fatalf("multi event = %#v", multi)
	}
	if !strings.Contains(multi.ToolName, "exec_command") || !strings.Contains(multi.ToolName, "view_image") {
		t.Fatalf("multi tool names = %q", multi.ToolName)
	}

	legacy := got.Events[3]
	if legacy.Kind != "tool" || legacy.ToolName != "view_image" {
		t.Fatalf("legacy event = %#v", legacy)
	}
}

func TestIsCodexExecWrapperTool(t *testing.T) {
	if !isCodexExecWrapperTool("exec") || !isCodexExecWrapperTool("functions.exec") {
		t.Fatal("expected exec wrappers to match")
	}
	if isCodexExecWrapperTool("exec_command") || isCodexExecWrapperTool("apply_patch") {
		t.Fatal("direct tools must not match wrapper detector")
	}
}
