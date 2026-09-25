/** @module PagesTest @description Covers workspace pages: aggregate analytics, comparison, local health and the connection workspace. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import type {AnalyticsReport} from "../types";
import {Analytics} from "./Analytics";
import {Compare} from "./Compare";
import {Connections} from "./Connections";
import {Diagnostics} from "./Diagnostics";
import {RemoteAccess} from "./RemoteAccess";
import {RemoteReview} from "./RemoteReview";
import {first,json,metrics,second} from "./testFixtures";

afterEach(()=>{cleanup();vi.unstubAllGlobals()});

const report:AnalyticsReport={summary:{sessions:2,turns:4,duration_ms:60000,input_tokens:400,cached_input_tokens:100,output_tokens:80,reasoning_tokens:20,cache_ratio:.25,tool_calls:6,mcp_calls:2,app_tool_calls:1,failed_tool_calls:1,findings:2},inventory:{stored_records:6,included_sessions:2,internal_review_records:3,telemetry_only_records:1,parser_warning_sessions:2,parser_warnings:4,skipped_record_warnings:1},daily:[{date:"2026-09-19",sessions:2,input_tokens:400,cached_tokens:100,output_tokens:80,tool_calls:6,failed_tool_calls:1}],sources:[{name:"codex",sessions:2,input_tokens:400,tool_calls:6,failed_tool_calls:1}],models:[{name:"gpt-6",sessions:2,input_tokens:400,tool_calls:6,failed_tool_calls:1}],repositories:[{name:"sessionsift",sessions:2,input_tokens:400,tool_calls:6,failed_tool_calls:1}],tools:[{name:"exec",transport:"builtin",calls:3,failed:1},{name:"create_design",transport:"mcp",server:"canva",calls:1,failed:0}],findings:[{type:"possible_scope_correction",severity:"warning",confidence:"possible",count:2,sessions:2}],recommendations:[{id:"scope",title:"Clarify scope earlier",summary:"State the target component.",confidence:"derived",evidence:["2 findings"]}]};

describe("analytics",()=>{
  it("shows privacy-safe aggregates, breakdowns, tools, recommendations and finding patterns",()=>{
    render(<Analytics report={report}/>);
    expect(screen.getByRole("heading",{name:"The pattern, not the transcript."})).toBeInTheDocument();
    expect(screen.getByText("25%")).toBeInTheDocument();
    expect(screen.getByText("Codex")).toBeInTheDocument();
    expect(screen.getByText("exec")).toBeInTheDocument();
    expect(screen.getByText("canva")).toBeInTheDocument();
    expect(screen.getByText("Clarify scope earlier")).toBeInTheDocument();
    expect(screen.getByText("possible scope correction")).toBeInTheDocument();
    expect(screen.getByLabelText("Input tokens by start date")).toBeInTheDocument();
    expect(screen.getByText(/^2 sessions from/)).toBeInTheDocument();
    expect(screen.getByText(/2 analyzed conversations from 6 stored records/)).toBeInTheDocument();
    expect(screen.getByText(/3 internal auto-reviews and 1 telemetry-only placeholder/)).toBeInTheDocument();
    expect(screen.getByText(/4 parser notices across 2 sessions; 1 may include skipped records/)).toBeInTheDocument();
  });

  it("counts a single session in the singular",()=>{
    render(<Analytics report={{...report,summary:{...report.summary,sessions:1},sources:[{name:"chatgpt",sessions:1,input_tokens:10,tool_calls:0,failed_tool_calls:0}],models:[],repositories:[]}}/>);
    expect(screen.getByText(/^1 session from/)).toBeInTheDocument();
    expect(screen.getByText(/^session ·/)).toBeInTheDocument();
  });

  it("explains an empty library",()=>{
    render(<Analytics report={{...report,daily:[],sources:[],models:[],repositories:[],tools:[],findings:[],recommendations:[]}}/>);
    expect(screen.getByText("Trends appear after conversations are imported.")).toBeInTheDocument();
    expect(screen.getByText("No tools observed yet.")).toBeInTheDocument();
    expect(screen.getByText("No recurring pattern currently crosses a recommendation threshold.")).toBeInTheDocument();
    expect(screen.getAllByText("No observed data yet.")).toHaveLength(3);
  });
});

describe("compare",()=>{
  it("loads observed metrics and differences and reloads when a side changes",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>json({a:first,b:second,metrics_a:metrics,metrics_b:{...metrics,time_to_first_edit_ms:120000,usage_measured_turns:0},differences:["A used one call; B used one call."]})));
    render(<Compare sessions={[first,second]} initial="a"/>);
    expect(await screen.findByText("A used one call; B used one call.")).toBeInTheDocument();
    expect(screen.getByText("First edit").parentElement).toHaveTextContent("First editNot observed2m");
    expect(screen.getByText("+10.0 points")).toBeInTheDocument();
    fireEvent.change(screen.getAllByRole("combobox")[0],{target:{value:"b"}});
    expect(await screen.findByText("Select two different sessions to compare.")).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledOnce();
  });

  it("requires distinct sessions and reports failures",async()=>{
    const {unmount}=render(<Compare sessions={[first]} initial="a"/>);
    expect(screen.getByText("Select two different sessions to compare.")).toBeInTheDocument();
    unmount();
    vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
    render(<Compare sessions={[first,second]}/>);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
  });
});

describe("diagnostics",()=>{
  it("counts every source, internal reviews and parser warnings",()=>{
    render(<Diagnostics sessions={[first,{...second,archived:true},{...second,id:"c",source:"claude_code"},{...second,id:"g",source:"chatgpt"},{...second,id:"i",session_kind:"internal_review",warnings:["w"]}]}/>);
    expect(screen.getByText("Claude Code sessions").parentElement?.nextElementSibling).toHaveTextContent("1");
    expect(screen.getByText("Archived Codex sessions").parentElement?.nextElementSibling).toHaveTextContent("1");
    expect(screen.getByText("Parser notices").closest(".health-row")).toHaveClass("attention");
    expect(screen.getByText("ChatGPT conversations").closest(".health-row")).toHaveClass("ok");
    expect(screen.getByText("Claude.ai conversations").closest(".health-row")).toHaveClass("optional");
    expect(screen.getByRole("button",{name:"Preview safe report"})).toBeInTheDocument();
    expect(screen.getByText("OpenTelemetry logs").parentElement?.nextElementSibling).toHaveTextContent("Checking…");
  });

  // Regression anchor for CU-20260924-008 (ported from D-20260924-052): collector activity is observed, not assumed.
  it("reports live collector activity for OpenTelemetry, Codex hooks and Claude Code hooks, and what data can show",()=>{
    const recent=new Date().toISOString();
    render(<Diagnostics sessions={[first]} status={{collector_running:true,otel_active:true,otel_events:803,latest_otel_event_at:recent,hook_active:false,hook_events:120,latest_hook_event_at:"2026-01-01T00:00:00Z",claude_hook_active:true,claude_hook_events:4,latest_claude_hook_event_at:recent,metrics_receiver:"acknowledge_only"}}/>);
    const value=(label:string)=>screen.getByText(label).parentElement?.nextElementSibling;
    expect(value("OpenTelemetry logs")).toHaveTextContent("Active now");
    expect(value("Codex lifecycle hooks")).toHaveTextContent("Idle");
    expect(value("Claude Code hooks")).toHaveTextContent("Active now");
    expect(value("Collector endpoint")).toHaveTextContent("Online");
    expect(screen.getByText(/803 structured telemetry events · last received/)).toBeInTheDocument();
    expect(screen.getByText("Codex lifecycle hooks").closest(".health-row")).toHaveClass("optional");
    expect(screen.getByText(/Metrics batches are validated and acknowledged/)).toBeInTheDocument();
    expect(screen.getByRole("heading",{name:"What SessionSift can show"})).toBeInTheDocument();
  });

  it("says when capture activity cannot be read and when nothing has arrived",()=>{
    const {rerender}=render(<Diagnostics sessions={[]} statusError="Error: offline"/>);
    expect(screen.getByText("OpenTelemetry logs").parentElement?.nextElementSibling).toHaveTextContent("Unavailable");
    expect(screen.getByText("Collector endpoint").closest(".health-row")).toHaveClass("attention");
    rerender(<Diagnostics sessions={[]} status={{collector_running:true,otel_active:false,otel_events:0,hook_active:false,hook_events:0,metrics_receiver:"acknowledge_only"}}/>);
    expect(screen.getAllByText(/no events received yet/)).toHaveLength(3);
  });
});

describe("connection workspace",()=>{
  it("links to the bridge support page when this device is connected",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>{const path=String(input);if(path.endsWith("/bridge"))return json({connected:true,url:"https://bridge.example",device_id:"d"});if(path.endsWith("/status"))return json({device_id:"d",pending_pairings:[],connections:[],audit:[]});return json([])}));
    render(<Connections/>);
    expect(await screen.findByRole("link",{name:/bridge.example\/support/})).toHaveAttribute("href","https://bridge.example/support");
  });

  it("explains support-code entry when no bridge is known",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
    render(<Connections/>);
    expect(await screen.findByText(/on the bridge the other person uses/)).toBeInTheDocument();
  });

  it("explains which access method to use and tells manual reviewers the portal is not the invitation flow",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
    render(<Connections/>);
    expect(await screen.findByRole("heading",{name:"Choose the access method"})).toBeInTheDocument();
    expect(screen.getByText(/Recommended for people/)).toBeInTheDocument();
    expect(screen.getByText(/Email invitations and support codes are the normal way/)).toBeInTheDocument();
    expect(screen.getByText(/This portal is for a reviewer using a manual pairing code/)).toBeInTheDocument();
  });

  it("creates, approves, audits and disconnects local pairing",async()=>{
    const status={device_id:"device_1",pending_pairings:[{code:"123-456",device_id:"device_1",reviewer:"reviewer",requested:["session.list"],created_at:"2026-01-01T00:00:00Z",expires_at:"2026-01-01T00:10:00Z",claimed:true,approved:false,used:false}],connections:[{id:"connection_1",grant_id:"grant_1",reviewer:"reviewer",connected_at:"2026-01-01T00:00:00Z",last_seen_at:"2026-01-01T00:00:00Z",active:true}],audit:Array.from({length:55},(_,index)=>({timestamp:"2026-01-01T00:01:00Z",client_id:index===54?"":"reviewer",operation:"session.list",allowed:index!==1,response_bytes:10,status:index===1?"denied":"ok"}))};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>{const path=String(input);if(path.endsWith("/pairings/approve"))return json({grant:{},token:"scoped-token"});if(path.endsWith("/pairings"))return json(status.pending_pairings[0],201);if(path.endsWith("/disconnect")||path.endsWith("/revoke")||path.endsWith("/deny"))return json({ok:true});return json(status)}));
    const {container}=render(<RemoteAccess/>);
    expect(await screen.findByText("Showing the latest 50 of 55 recorded operations.")).toBeInTheDocument();
    expect(container.querySelectorAll(".audit-row")).toHaveLength(50);
    expect(screen.getByText("unknown")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Create pairing code"}));
    expect(await screen.findByText("123-456")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Allow 30m"}));
    expect(await screen.findByText("scoped-token")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Deny"}));
    fireEvent.click(screen.getByRole("button",{name:"Disconnect & revoke"}));
    await waitFor(()=>expect(fetch).toHaveBeenCalledWith("/api/v1/remote/grants/revoke",expect.objectContaining({method:"POST"})));
    expect(fetch).toHaveBeenCalledWith("/api/v1/remote/pairings/deny",expect.objectContaining({method:"POST"}));
  });

  it("approves and revokes an OAuth identity for ChatGPT or Claude MCP",async()=>{
    const grant={id:"grant_oauth",reviewer:"owner@example.com",principal:"owner@example.com",auth_method:"oauth",operations:["session.list"],created_at:"2026-01-01T00:00:00Z",expires_at:"2026-01-15T00:00:00Z",revoked:false};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>{const path=String(input);if(path.endsWith("/oauth/grants"))return json(grant,201);if(path.endsWith("/revoke"))return json({revoked:true});return json({device_id:"device",pending_pairings:[],connections:[],oauth_grants:[grant],oauth_configured:true,audit:[]})}));
    render(<RemoteAccess/>);
    expect(await screen.findByText("Origin verifier ready")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("OAuth identity email"),{target:{value:"owner@example.com"}});
    fireEvent.click(screen.getByRole("button",{name:"Approve MCP access"}));
    await waitFor(()=>expect(fetch).toHaveBeenCalledWith("/api/v1/remote/oauth/grants",expect.objectContaining({method:"POST"})));
    fireEvent.click(screen.getByRole("button",{name:"Revoke owner@example.com"}));
    await waitFor(()=>expect(fetch).toHaveBeenCalledWith("/api/v1/remote/grants/revoke",expect.objectContaining({method:"POST"})));
  });

  it("shows remote access failures and empty states",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.reject(new Error("offline"))));
    render(<RemoteAccess/>);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
    for(const text of ["No approval is waiting.","No remote connections.","No remote operations yet.","No MCP identity is approved.","Origin config needed"])expect(screen.getByText(text)).toBeInTheDocument();
  });

  it("claims access and reads another workstation through a scoped grant",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>{const path=String(input);if(path.endsWith("/pairings/claim"))return json({code:"123-456"});const body=JSON.parse(String(init?.body||"{}"));if(body.operation==="session.list")return body.arguments.cursor?json({status:"ok",data:{sessions:[second],total:2,returned:1,offset:1,has_more:false}}):json({status:"ok",data:{sessions:[first],total:2,returned:1,offset:0,has_more:true,next_cursor:1}});return json({status:"ok",data:{...first,turns:[...first.turns!,{...first.turns![0],id:"t2",sequence:2,assistant_summary_redacted:undefined,user_prompt_redacted:undefined}]}})}));
    render(<RemoteReview/>);
    fireEvent.change(screen.getByLabelText("Pairing code"),{target:{value:"123-456"}});
    fireEvent.change(screen.getByLabelText("Reviewer name"),{target:{value:"A reviewer"}});
    fireEvent.click(screen.getByRole("button",{name:"Send request"}));
    expect(await screen.findByText(/workstation owner/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Scoped token"),{target:{value:"token"}});
    fireEvent.click(screen.getByRole("button",{name:"Load approved sessions"}));
    expect(await screen.findByText("Beta session")).toBeInTheDocument();
    fireEvent.click(screen.getByText("Alpha session"));
    expect(await screen.findByText("Inspect auth")).toBeInTheDocument();
    expect(screen.getByText("Prompt content unavailable.")).toBeInTheDocument();
  });

  it("reports reviewer portal failures",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.resolve(new Response("",{status:401}))));
    render(<RemoteReview/>);
    fireEvent.click(screen.getByRole("button",{name:"Send request"}));
    expect(await screen.findByRole("alert")).toHaveTextContent("401");
    fireEvent.click(screen.getByRole("button",{name:"Load approved sessions"}));
    await waitFor(()=>expect(screen.getByRole("alert")).toHaveTextContent("Remote API returned 401"));
  });
});
