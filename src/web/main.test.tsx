/** @module AppTest @description Exercises the live local-data UI workflow. */
import "@testing-library/jest-dom/vitest";
import {cleanup,fireEvent,render,screen,waitFor} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {App} from "./App";

const metrics={duration_ms:600000,turn_count:2,input_tokens:142000,cached_input_tokens:28000,output_tokens:18000,reasoning_tokens:4000,cache_ratio:.2,tool_calls:3,builtin_tool_calls:2,mcp_calls:1,app_tool_calls:0,unique_tools:3,unique_mcp_servers:1,failed_tool_calls:1,reads:2,searches:0,edits:1,validations:0,compactions:0,exploration_ratio:.67,finding_count:1};
const summary={id:"s1",title:"Fix authentication middleware",repo_display_name:"sessionsift",model:"gpt-test",started_at:"2026-09-19T10:00:00Z",ended_at:"2026-09-19T10:10:00Z",status:"completed",archived:false,metrics};
const detail={...summary,turns:[{id:"t1",sequence:1,started_at:"2026-09-19T10:00:00Z",ended_at:"2026-09-19T10:05:00Z",user_prompt_redacted:"Fix the auth bug",input_tokens:100000,cached_input_tokens:20000,output_tokens:10000,reasoning_tokens:2000},{id:"t2",sequence:2,started_at:"2026-09-19T10:06:00Z",ended_at:"2026-09-19T10:10:00Z",user_prompt_redacted:"Only change Cognito",input_tokens:42000,cached_input_tokens:8000,output_tokens:8000,reasoning_tokens:2000}],tool_calls:[{id:"c1",turn_id:"t1",timestamp:"2026-09-19T10:01:00Z",tool_name:"read_file",tool_category:"read",target_path_display:"src/auth.ts",success:true},{id:"c2",turn_id:"t1",timestamp:"2026-09-19T10:02:00Z",tool_name:"exec",tool_category:"validation",command_signature:"npm test",success:false}],findings:[{id:"f1",turn_id:"t1",finding_type:"repeated_file_read",severity:"info",confidence:"observed" as const,title:"Repeated read",summary:"src/auth.ts was read three times.",evidence:["c1"],metric:{count:3}}],events:[]};

function response(value:unknown){return Promise.resolve(new Response(JSON.stringify(value),{status:200,headers:{"Content-Type":"application/json"}}))}
afterEach(()=>{cleanup();vi.unstubAllGlobals();window.history.replaceState({},"","/")});

describe("App",()=>{
  it("loads sessions, opens turn evidence, compares, and shows diagnostics",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>{const path=String(input);if(path.startsWith("/api/v1/compare"))return response({a:detail,b:detail,metrics_a:metrics,metrics_b:metrics,differences:["Session A used 3 tool calls; Session B used 3."]});if(path==="/api/v1/sessions/s1")return response(detail);return response([summary]);}));
    const {container}=render(<App/>);
    expect(await screen.findByRole("heading",{name:"Fix authentication middleware"})).toBeInTheDocument();
    expect(container.firstElementChild).toHaveClass("sessionLayout");
    fireEvent.click(screen.getByText("Fix the auth bug"));
    expect(screen.getByRole("dialog")).toHaveTextContent("Conversation turn");
    fireEvent.click(screen.getByLabelText("Close turn detail"));
    fireEvent.click(screen.getByRole("button",{name:/^Compare$/}));
    expect(await screen.findByRole("heading",{name:"Compare sessions"})).toBeInTheDocument();
    expect(container.firstElementChild).toHaveClass("focusLayout");
    fireEvent.click(screen.getByRole("link",{name:/Diagnostics/}));
    expect(screen.getByRole("heading",{name:"Diagnostics"})).toBeInTheDocument();
    expect(container.firstElementChild).toHaveClass("focusLayout");
    await waitFor(()=>expect(fetch).toHaveBeenCalled());
  });

  it("shows a useful local API error",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.resolve(new Response("",{status:503}))));
    render(<App/>);
    expect(await screen.findByText("Local API unavailable")).toBeInTheDocument();
  });

  it("uses history navigation for deep links and back-forward transitions",async()=>{
    window.history.replaceState({},"","/sessions/s1");
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input)==="/api/v1/sessions/s1"?response(detail):response([summary])));
    render(<App/>);
    expect(await screen.findByRole("heading",{name:"Fix authentication middleware"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link",{name:/Compare/}));
    expect(window.location.pathname).toBe("/compare");
    window.history.back();
    window.dispatchEvent(new PopStateEvent("popstate"));
    await waitFor(()=>expect(window.location.pathname).toBe("/sessions/s1"));
  });
});
