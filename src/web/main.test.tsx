/** @module AppTest @description Exercises the workspace shell against a stubbed local API: routing, loading, shortcuts, theme, inspector and dialogs. */
import "@testing-library/jest-dom/vitest";
import {cleanup,configure,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {App} from "./App";

// Whole-app renders load sessions, then detail, then lazy panes; under a busy machine the first paint can exceed the 1s default.
configure({asyncUtilTimeout:5000});

const metrics={duration_ms:600000,elapsed_duration_ms:600000,usage_delta_percent:0,usage_measured_turns:0,turn_count:2,input_tokens:142000,cached_input_tokens:28000,output_tokens:18000,reasoning_tokens:4000,cache_ratio:.2,tool_calls:2,builtin_tool_calls:2,mcp_calls:0,app_tool_calls:0,unique_tools:2,unique_mcp_servers:0,failed_tool_calls:1,reads:1,searches:0,edits:0,validations:1,compactions:0,exploration_ratio:.5,finding_count:1};
const summary={id:"s1",title:"Fix authentication middleware",repo_display_name:"sessionsift",model:"gpt-test",started_at:"2026-09-19T10:00:00Z",ended_at:"2026-09-19T10:10:00Z",status:"completed",archived:false,metrics};
const other={...summary,id:"s2",title:"Tune the cache",repo_display_name:"cache"};
const detail={...summary,turns:[{id:"t1",sequence:1,started_at:"2026-09-19T10:00:00Z",ended_at:"2026-09-19T10:05:00Z",user_prompt_redacted:"Fix the auth bug",assistant_summary_redacted:"Fixed the token check.",input_tokens:100000,cached_input_tokens:20000,output_tokens:10000,reasoning_tokens:2000},{id:"t2",sequence:2,started_at:"2026-09-19T10:06:00Z",ended_at:"2026-09-19T10:10:00Z",user_prompt_redacted:"Only change Cognito",input_tokens:42000,cached_input_tokens:8000,output_tokens:8000,reasoning_tokens:2000}],tool_calls:[{id:"c1",turn_id:"t1",timestamp:"2026-09-19T10:01:00Z",tool_name:"read_file",tool_category:"read",target_path_display:"src/auth.ts",success:true},{id:"c2",turn_id:"t1",timestamp:"2026-09-19T10:02:00Z",tool_name:"exec",tool_category:"validation",command_signature:"npm test",success:false}],findings:[{id:"f1",turn_id:"t1",finding_type:"repeated_file_read",severity:"info",confidence:"observed" as const,title:"Repeated read",summary:"src/auth.ts was read three times.",evidence:["c1"],metric:{count:3}}],events:[]};
const analytics={summary:{sessions:1,turns:2,duration_ms:600000,input_tokens:142000,cached_input_tokens:28000,output_tokens:18000,reasoning_tokens:4000,cache_ratio:.2,tool_calls:3,mcp_calls:1,app_tool_calls:0,failed_tool_calls:1,findings:1},daily:[],sources:[],models:[],repositories:[],tools:[],findings:[],recommendations:[]};

function response(value:unknown,status=200){return Promise.resolve(new Response(JSON.stringify(value),{status,headers:{"Content-Type":"application/json"}}))}
type Routes={sessions?:unknown[];support?:unknown[];detail?:()=>Promise<Response>;list?:()=>Promise<Response>;diagnostics?:()=>Promise<Response>};
function stubApi(routes:Routes={}){
  const fetchMock=vi.fn((input:RequestInfo|URL)=>{
    const path=String(input);
    if(path.startsWith("/api/v1/sessions?"))return routes.list?routes.list():response(routes.sessions??[summary,other]);
    if(path==="/api/v1/sessions/s1")return routes.detail?routes.detail():response(detail);
    if(path==="/api/v1/sessions/s2")return response({...detail,id:"s2",title:"Tune the cache"});
    if(path==="/api/v1/analytics")return response(analytics);
    if(path==="/api/v1/diagnostics")return routes.diagnostics?routes.diagnostics():response({collector_running:true,otel_active:true,otel_events:12,latest_otel_event_at:new Date().toISOString(),hook_active:false,hook_events:0,metrics_receiver:"acknowledge_only"});
    if(path.startsWith("/api/v1/compare"))return response({a:detail,b:detail,metrics_a:metrics,metrics_b:metrics,differences:["Session A used 2 tool calls; Session B used 2."]});
    if(path==="/api/v1/remote/support")return response(routes.support??[]);
    if(path==="/api/v1/remote/bridge")return response({connected:false,device_id:"device_1"});
    if(path==="/api/v1/remote/status")return response({device_id:"device_1",pending_pairings:[],connections:[],audit:[]});
    return response([]);
  });
  vi.stubGlobal("fetch",fetchMock);
  return fetchMock;
}
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();window.history.replaceState({},"","/");window.localStorage.clear();delete document.documentElement.dataset.theme});

describe("App",()=>{
  it("keeps the public root URL stable after selecting the first session",async()=>{
    stubApi();
    render(<App/>);
    expect(await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"},{timeout:5000})).toBeInTheDocument();
    expect(window.location.pathname).toBe("/");
    expect(screen.getByText("Local library / 2 sessions")).toBeInTheDocument();
  });

  it("opens evidence from the conversation and moves between lenses and pages with history routes",async()=>{
    stubApi();
    const {container}=render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.click(await screen.findByRole("button",{name:"Inspect prompt 1"}));
    expect(within(screen.getByRole("complementary",{name:"Session inspector"})).getByRole("heading",{name:"Fix the auth bug"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab",{name:/Timeline/}));
    expect(window.location.pathname).toBe("/sessions/s1/timeline");
    expect(screen.getByRole("heading",{name:"The work, in order."})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab",{name:/Tools/}));
    expect(await screen.findAllByText("Repeated read")).toHaveLength(2);
    fireEvent.click(within(screen.getByRole("complementary",{name:"Session inspector"})).getByRole("tab",{name:"Context"}));
    fireEvent.click(screen.getByRole("button",{name:/Compare with another session/}));
    expect(window.location.pathname).toBe("/compare");
    expect(await screen.findByText("Session A used 2 tool calls; Session B used 2.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("link",{name:"Diagnostics"}));
    expect(screen.getByRole("heading",{level:1,name:"Diagnostics"})).toBeInTheDocument();
    expect(container.querySelector(".statusbar")).toHaveTextContent("Diagnostics");
    fireEvent.click(within(screen.getByRole("list",{name:"Sessions"})).getByText("Tune the cache"));
    expect(await screen.findByRole("heading",{level:1,name:"Tune the cache"})).toBeInTheDocument();
    expect(window.location.pathname).toBe("/sessions/s2");
  });

  it("loads analytics through Ajax navigation",async()=>{
    stubApi();
    render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.click(screen.getByRole("link",{name:"Analytics"}));
    expect(await screen.findByRole("heading",{name:"The pattern, not the transcript."})).toBeInTheDocument();
    expect(window.location.pathname).toBe("/analytics");
  });

  it("shows a useful local API error",async()=>{
    vi.stubGlobal("fetch",vi.fn(()=>Promise.resolve(new Response("",{status:503}))));
    render(<App/>);
    expect(await screen.findByText("Local API unavailable")).toBeInTheDocument();
    expect(screen.getByText("Local agent unreachable")).toBeInTheDocument();
  });

  it("uses history navigation for deep links, legacy paths and back-forward transitions",async()=>{
    window.history.replaceState({},"","/sessions/s1/review");
    stubApi();
    render(<App/>);
    expect(await screen.findByRole("heading",{name:"A review, not another chat."})).toBeInTheDocument();
    expect(screen.getByRole("tab",{name:/Review/})).toHaveAttribute("aria-selected","true");
    fireEvent.click(screen.getByRole("link",{name:"Compare"}));
    expect(window.location.pathname).toBe("/compare");
    window.history.pushState({},"","/remote-access");
    window.dispatchEvent(new PopStateEvent("popstate"));
    expect(await screen.findByRole("heading",{level:1,name:"Connections"})).toBeInTheDocument();
    window.history.pushState({},"","/sessions/s1");
    window.dispatchEvent(new PopStateEvent("popstate"));
    expect(await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"})).toBeInTheDocument();
  });

  it("renders standalone detail even when the library refresh has not completed",async()=>{
    window.history.replaceState({},"","/sessions/s1");
    let releaseSessions:(value:Response)=>void=()=>{};
    const pending=new Promise<Response>(resolve=>{releaseSessions=resolve});
    const fetchMock=stubApi({list:()=>pending});
    render(<App/>);
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/sessions/s1",expect.anything()));
    expect(await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"})).toBeInTheDocument();
    releaseSessions(new Response(JSON.stringify([summary]),{status:200,headers:{"Content-Type":"application/json"}}));
    expect(await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"})).toBeInTheDocument();
  });

  it("explains an empty library and how to import",async()=>{
    stubApi({sessions:[]});
    render(<App/>);
    expect(await screen.findByRole("heading",{name:"No conversations yet"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"How to import"}));
    expect(screen.getByRole("heading",{name:"Bring your conversations together"})).toBeInTheDocument();
  });

  it("supports the command palette, library search, focus mode and Escape",async()=>{
    stubApi();
    vi.spyOn(window,"requestAnimationFrame").mockImplementation(callback=>{callback(0);return 0});
    Element.prototype.scrollIntoView=vi.fn();
    const {container}=render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.keyDown(window,{key:"k",ctrlKey:true});
    const input=screen.getByRole("combobox",{name:"Search commands and sessions"});
    fireEvent.change(input,{target:{value:"analytics"}});
    fireEvent.keyDown(input,{key:"Enter"});
    expect(window.location.pathname).toBe("/analytics");
    fireEvent.keyDown(window,{key:"k",metaKey:true});
    fireEvent.keyDown(window,{key:"k",metaKey:true});
    expect(screen.queryByRole("combobox",{name:"Search commands and sessions"})).not.toBeInTheDocument();
    fireEvent.keyDown(window,{key:"/"});
    expect(screen.getByLabelText("Search sessions")).toHaveFocus();
    fireEvent.keyDown(screen.getByLabelText("Search sessions"),{key:"/"});
    fireEvent.click(within(screen.getByRole("list",{name:"Sessions"})).getByText("Fix authentication middleware"));
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.keyDown(window,{key:"f",ctrlKey:true,shiftKey:true});
    expect(container.querySelector(".app")).toHaveClass("focus-mode");
    fireEvent.keyDown(window,{key:"Escape"});
    expect(container.querySelector(".app")).not.toHaveClass("focus-mode");
    fireEvent.click(screen.getByRole("button",{name:"Enter focus mode"}));
    fireEvent.click(screen.getByRole("button",{name:"Show inspector"}));
    expect(container.querySelector(".app")).not.toHaveClass("focus-mode");
  });

  it("toggles theme, the docked inspector, the library drawer and the share dialog",async()=>{
    stubApi({support:[{id:"support_1",session_id:"s1",role:"view",operations:[],created_at:new Date().toISOString(),code_expires_at:new Date(Date.now()+300000).toISOString(),access_minutes:60,failed_attempts:0,status:"waiting"}]});
    const {container}=render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    const themeButton=screen.getByRole("button",{name:/Use (dark|light) theme/});
    const before=document.documentElement.dataset.theme;
    fireEvent.click(themeButton);
    expect(document.documentElement.dataset.theme).not.toBe(before);
    expect(window.localStorage.getItem("sessionsift.theme")).toBe(document.documentElement.dataset.theme);
    fireEvent.click(screen.getByRole("button",{name:"Hide inspector"}));
    expect(container.querySelector(".app")).toHaveClass("inspector-hidden");
    fireEvent.click(screen.getByRole("button",{name:"Show inspector"}));
    expect(container.querySelector(".app")).not.toHaveClass("inspector-hidden");
    fireEvent.click(screen.getByRole("button",{name:"Close inspector"}));
    expect(container.querySelector(".app")).toHaveClass("inspector-hidden");
    fireEvent.click(await screen.findByRole("button",{name:"Inspect prompt 2"}));
    expect(container.querySelector(".app")).not.toHaveClass("inspector-hidden");
    fireEvent.click(screen.getByRole("button",{name:"Open session library"}));
    expect(container.querySelector(".app")).toHaveClass("sidebar-open");
    fireEvent.keyDown(window,{key:"Escape"});
    expect(container.querySelector(".app")).not.toHaveClass("sidebar-open");
    fireEvent.click(await screen.findByRole("button",{name:"Support: code waiting"}));
    expect(screen.getByRole("heading",{name:"Share this conversation"})).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button",{name:/Open Connections/}));
    expect(await screen.findByRole("heading",{level:1,name:"Connections"})).toBeInTheDocument();
  });

  it("opens the inspector as an overlay and the library as a drawer on narrow screens",async()=>{
    stubApi();
    vi.stubGlobal("matchMedia",(query:string)=>({matches:query.includes("max-width"),addEventListener:()=>{},removeEventListener:()=>{}}));
    vi.spyOn(window,"requestAnimationFrame").mockImplementation(callback=>{callback(0);return 0});
    const {container}=render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    const app=()=>container.querySelector(".app");
    expect(screen.getByRole("button",{name:"Show inspector"})).toHaveAttribute("aria-pressed","false");
    fireEvent.click(await screen.findByRole("button",{name:"Inspect prompt 1"}));
    expect(app()).toHaveClass("inspector-overlay");
    expect(screen.getByRole("button",{name:"Hide inspector"})).toHaveAttribute("aria-pressed","true");
    fireEvent.keyDown(window,{key:"Escape"});
    expect(app()).not.toHaveClass("inspector-overlay");
    fireEvent.click(screen.getByRole("button",{name:"Show inspector"}));
    expect(app()).toHaveClass("inspector-overlay");
    fireEvent.click(screen.getByRole("button",{name:"Add a note on this prompt"}));
    expect(app()).not.toHaveClass("inspector-overlay");
    fireEvent.click(screen.getByRole("button",{name:"Enter focus mode"}));
    fireEvent.click(screen.getByRole("button",{name:"Show inspector"}));
    expect(app()).toHaveClass("inspector-overlay");
    expect(app()).not.toHaveClass("focus-mode");
    fireEvent.click(container.querySelector(".mobile-scrim")!);
    fireEvent.keyDown(document.body,{key:"/"});
    expect(app()).toHaveClass("sidebar-open");
    fireEvent.click(container.querySelector(".mobile-scrim")!);
    expect(app()).not.toHaveClass("sidebar-open");
  });

  it("lists palette sessions in the library's default order, most recently updated first",async()=>{
    stubApi({sessions:[summary,{...other,ended_at:"2026-09-19T11:00:00Z"}]});
    render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.keyDown(window,{key:"k",ctrlKey:true});
    const options=screen.getAllByRole("option").map(option=>option.textContent||"");
    expect(options.findIndex(text=>text.includes("Tune the cache"))).toBeLessThan(options.findIndex(text=>text.includes("Fix authentication middleware")));
  });

  it("runs views, actions and pages from the command palette",async()=>{
    stubApi();
    vi.spyOn(window,"requestAnimationFrame").mockImplementation(callback=>{callback(0);return 0});
    Element.prototype.scrollIntoView=vi.fn();
    const {container}=render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    const run=(query:string)=>{fireEvent.keyDown(window,{key:"k",ctrlKey:true});const input=screen.getByRole("combobox",{name:"Search commands and sessions"});fireEvent.change(input,{target:{value:query}});fireEvent.keyDown(input,{key:"Enter"})};
    run("timeline");
    expect(window.location.pathname).toBe("/sessions/s1/timeline");
    for(const [query,view] of [["tools & evidence","tools"],["review notes","review"],["conversation","conversation"]] as const){run(query);expect(screen.getByRole("tab",{name:new RegExp(view,"i")})).toHaveAttribute("aria-selected","true")}
    run("toggle inspector");
    expect(container.querySelector(".app")).toHaveClass("inspector-hidden");
    run("toggle focus");
    expect(container.querySelector(".app")).toHaveClass("focus-mode");
    const before=document.documentElement.dataset.theme;
    run("toggle light");
    expect(document.documentElement.dataset.theme).not.toBe(before);
    run("import");
    expect(screen.getByRole("heading",{name:"Bring your conversations together"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Back to the workspace"}));
    run("share this");
    expect(screen.getByRole("heading",{name:"Share this conversation"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Close dialog"}));
    for(const [query,path] of [["connections","/connections"],["compare sessions","/compare"],["diagnostics","/diagnostics"],["analytics","/analytics"]] as const){run(query);expect(window.location.pathname).toBe(path)}
    run("tune the cache");
    expect(await screen.findByRole("heading",{level:1,name:"Tune the cache"})).toBeInTheDocument();
  });

  it("reads collector activity only on Diagnostics and keeps a failed probe on that page",async()=>{
    window.history.replaceState({},"","/diagnostics");
    const fetchMock=stubApi();
    const {unmount}=render(<App/>);
    expect(await screen.findByText("Active now")).toBeInTheDocument();
    expect(fetchMock).toHaveBeenCalledWith("/api/v1/diagnostics",expect.anything());
    unmount();
    stubApi({diagnostics:()=>Promise.reject(new Error("probe offline"))});
    render(<App/>);
    expect((await screen.findAllByText("Unavailable")).length).toBeGreaterThan(0);
    expect(screen.queryByText("Local API unavailable")).not.toBeInTheDocument();
  });

  it("adds a review note and records a decision through the local API",async()=>{
    const fetchMock=stubApi();
    render(<App/>);
    await screen.findByRole("heading",{level:1,name:"Fix authentication middleware"});
    fireEvent.change(await screen.findByLabelText("Review note text"),{target:{value:"Check Cognito"}});
    fireEvent.click(screen.getByRole("button",{name:/Add note/}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/annotations",expect.objectContaining({method:"POST",body:JSON.stringify({session_id:"s1",author:"local owner",body:"Check Cognito"})})));
    expect(await screen.findByText(/Review note added on this device/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("tab",{name:/Review/}));
    fireEvent.click(screen.getByRole("button",{name:"Mark reviewed"}));
    await waitFor(()=>expect(fetchMock).toHaveBeenCalledWith("/api/v1/remote/reviews",expect.objectContaining({method:"POST"})));
  });
});
