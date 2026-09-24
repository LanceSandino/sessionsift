/** @module TestFixtures @description Shared session records for component tests; synthetic, redacted-shaped data only. */
import {vi} from "vitest";
import type {Metrics,Session,ToolCall,Turn} from "../types";
import type {WorkspaceActions} from "../workspace";

export const metrics:Metrics={duration_ms:60000,elapsed_duration_ms:120000,usage_delta_percent:10,usage_measured_turns:1,turn_count:1,input_tokens:100,cached_input_tokens:50,output_tokens:20,reasoning_tokens:5,cache_ratio:.5,tool_calls:1,builtin_tool_calls:1,mcp_calls:0,app_tool_calls:0,unique_tools:1,unique_mcp_servers:0,failed_tool_calls:0,reads:1,searches:0,edits:0,validations:0,compactions:0,exploration_ratio:1,finding_count:0};

export const turn:Turn={id:"t1",sequence:1,started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:01:00Z",user_prompt_redacted:"Inspect auth",assistant_summary_redacted:"I inspected the authentication flow and found the failing branch.",model:"gpt-6-astra",reasoning_effort:"high",rate_limit_used_start:84,rate_limit_used_end:94,rate_limit_window_minutes:300,input_tokens:100,cached_input_tokens:50,output_tokens:20,reasoning_tokens:5};

export const readCall:ToolCall={id:"c",turn_id:"t1",timestamp:"2026-01-01T00:00:30Z",tool_name:"read_file",tool_category:"read",success:true,target_path_display:"src/auth.ts",result_preview_redacted:"auth contents",arguments_redacted:"{\"path\":\"src/auth.ts\"}"};

export const first:Session={id:"a",title:"Alpha session",title_source:"renamed",repo_display_name:"repo-a",model:"gpt-a",originator:"Codex Desktop",surface:"vscode",thread_source:"user",runtime_location:"local",approval_mode:"on-request",sandbox_policy:"managed",network_access:"restricted",git_branch:"fix/auth",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:02:00Z",status:"completed",archived:false,metrics,turns:[turn],tool_calls:[readCall],findings:[],events:[{id:"steer",turn_id:"t1",timestamp:"2026-01-01T00:00:45Z",event_type:"message",event_subtype:"steering_or_queued",payload_redacted:{text:"Also push it"}}]};

export const second:Session={...first,id:"b",title:"Beta session",repo_display_name:"repo-b",model:"gpt-b"};

/** A session with a failed validation that a later edit and validation recovered from. */
export function workedSession():Session{
  const calls:ToolCall[]=[
    {id:"grep",turn_id:"t1",timestamp:"2026-01-01T00:00:10Z",tool_name:"Grep",tool_category:"search",success:true,target_path_display:"checkout"},
    {id:"test-fail",turn_id:"t1",timestamp:"2026-01-01T00:00:20Z",tool_name:"Bash",tool_category:"validation",success:false,exit_code:1,command_signature:"go test -race ./...",arguments_redacted:"go test -race ./...",result_preview_redacted:"WARNING: DATA RACE\nFAIL"},
    {id:"edit",turn_id:"t1",timestamp:"2026-01-01T00:00:30Z",tool_name:"Edit",tool_category:"edit",success:true,target_path_display:"cart.go"},
    {id:"test-pass",turn_id:"t1",timestamp:"2026-01-01T00:00:40Z",tool_name:"Bash",tool_category:"validation",success:true,command_signature:"go test -race -count=20 ./..."},
    {id:"pr",turn_id:"t2",timestamp:"2026-01-01T00:03:10Z",tool_name:"mcp__github__create_pull_request",tool_category:"other",tool_transport:"mcp",tool_server:"github",success:true},
  ];
  const later:Turn={...turn,id:"t2",sequence:2,started_at:"2026-01-01T00:03:00Z",ended_at:"2026-01-01T00:04:00Z",user_prompt_redacted:"Open a PR",assistant_summary_redacted:"- Opened **PR #482**",reasoning_effort:undefined,rate_limit_used_start:undefined,rate_limit_used_end:undefined};
  return {...first,id:"worked",source:"claude_code",turns:[turn,later],tool_calls:calls,metrics:{...metrics,turn_count:2,tool_calls:5,failed_tool_calls:1},findings:[{id:"f1",turn_id:"t1",finding_type:"failed_tool_loop",severity:"warning",confidence:"observed",title:"A validation failed before passing",summary:"go test failed once.",evidence:["test-fail","event-x"],metric:{count:1}}],events:[{id:"compact",turn_id:"t2",timestamp:"2026-01-01T00:03:30Z",event_type:"compaction",event_subtype:"compact_boundary"}]};
}

export function actions():WorkspaceActions&{[key:string]:ReturnType<typeof vi.fn>}{
  return {selectEvidence:vi.fn(),jumpToTurn:vi.fn(),noteOn:vi.fn(),setView:vi.fn()};
}

export function json(value:unknown,status=200){return Promise.resolve(new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json"}}))}
