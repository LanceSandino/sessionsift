/** @module LibraryTest @description Covers the application rail, session library, top bar and status bar. */
import "@testing-library/jest-dom/vitest";
import React,{createRef} from "react";
import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import type {Session} from "../types";
import {AppRail} from "./AppRail";
import {SessionNavigator} from "./SessionNavigator";
import {StatusBar} from "./StatusBar";
import {TopBar} from "./TopBar";
import {first,metrics,second} from "./testFixtures";

afterEach(cleanup);
const now=Date.parse("2026-01-01T12:00:00Z");
function library(sessions:Session[],overrides:Partial<React.ComponentProps<typeof SessionNavigator>>={}){
  const props={sessions,selected:"a",page:"sessions" as const,now,searchRef:createRef<HTMLInputElement>(),onSelect:vi.fn(),onPage:vi.fn(),onImport:vi.fn(),onClose:vi.fn(),...overrides};
  render(<SessionNavigator {...props}/>);
  return props;
}

describe("session library",()=>{
  it("searches, marks the current session and selects another",()=>{
    const props=library([first,second]);
    const list=within(screen.getByRole("list",{name:"Sessions"}));
    expect(list.getByText("Alpha session").closest("button")).toHaveAttribute("aria-current","true");
    expect(screen.getByLabelText("2 shown")).toHaveTextContent("02");
    fireEvent.change(screen.getByLabelText("Search sessions"),{target:{value:"beta"}});
    expect(list.queryByText("Alpha session")).not.toBeInTheDocument();
    fireEvent.click(list.getByText("Beta session"));
    expect(props.onSelect).toHaveBeenCalledWith("b");
  });

  // Regression anchor for CU-20260924-008 (ported from D-20260924-051): recent work first, with explicit alternatives.
  it("sorts by last update by default, offers the other orders and shows each row's time",()=>{
    const older:Session={...first,id:"older",title:"Older",started_at:"2025-12-30T08:00:00Z",ended_at:"2025-12-30T09:00:00Z"};
    const latest:Session={...second,id:"latest",title:"Latest",started_at:"2025-12-20T08:00:00Z",ended_at:"2026-01-01T10:30:00Z"};
    library([older,latest]);
    const rows=()=>within(screen.getByRole("list",{name:"Sessions"})).getAllByRole("button").map(button=>button.querySelector("strong")?.textContent);
    const sort=screen.getByLabelText("Sort conversations");
    expect(sort).toHaveValue("updated_desc");
    expect(rows()).toEqual(["Latest","Older"]);
    expect(screen.getByText("Today",{selector:".list-label"})).toBeInTheDocument();
    const time=screen.getByText("Latest").closest("button")!.querySelector("time")!;
    expect(time).toHaveAttribute("dateTime","2026-01-01T10:30:00Z");
    expect(time.getAttribute("title")).toMatch(/^Updated /);
    fireEvent.change(sort,{target:{value:"started_desc"}});
    expect(rows()).toEqual(["Older","Latest"]);
    expect(screen.getByText("Older").closest("button")!.querySelector("time")!.getAttribute("title")).toMatch(/^Created /);
    fireEvent.change(sort,{target:{value:"title_asc"}});
    expect(screen.getByText("Title A–Z",{selector:".list-label"})).toBeInTheDocument();
    expect(rows()).toEqual(["Latest","Older"]);
  });

  it("filters by source chips, including every chat product under Chats",()=>{
    const chat:Session={...second,id:"chat",title:"Chat conversation",source:"chatgpt",session_kind:"chat"};
    const code:Session={...second,id:"code",title:"Code session",source:"claude_code"};
    library([first,chat,code]);
    const list=within(screen.getByRole("list",{name:"Sessions"}));
    fireEvent.click(screen.getByRole("button",{name:"Chats"}));
    expect(screen.getByRole("button",{name:"Chats"})).toHaveAttribute("aria-pressed","true");
    expect(list.getByText("Chat conversation")).toBeInTheDocument();
    expect(list.queryByText("Alpha session")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Claude Code"}));
    expect(list.getByText("Code session")).toBeInTheDocument();
    expect(list.queryByText("Chat conversation")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Codex"}));
    expect(list.getByText("Alpha session")).toBeInTheDocument();
    fireEvent.click(within(screen.getByRole("group",{name:"Filter by source"})).getByRole("button",{name:"All"}));
    expect(list.getAllByRole("listitem")).toHaveLength(3);
  });

  it("opens detailed filters for provenance, repository, model, date and metrics",()=>{
    const high={...metrics,input_tokens:120000,failed_tool_calls:2,finding_count:1};
    const internal:Session={...second,id:"review",title:"Auto-review approval assessment",session_kind:"internal_review",metrics:high};
    library([first,second,internal]);
    const list=within(screen.getByRole("list",{name:"Sessions"}));
    const toggle=screen.getByRole("button",{name:"Filters"});
    expect(toggle).toHaveAttribute("aria-expanded","false");
    fireEvent.click(toggle);
    expect(screen.getByRole("button",{name:"Hide filters"})).toHaveAttribute("aria-expanded","true");
    expect(list.queryByText("Auto-review approval assessment")).not.toBeInTheDocument();
    expect(screen.getByRole("button",{name:"You"})).toHaveAttribute("aria-pressed","true");
    fireEvent.click(screen.getByRole("button",{name:"Reviews"}));
    expect(list.getByText("Auto-review approval assessment")).toBeInTheDocument();
    expect(list.getByText("review")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Repository filter"),{target:{value:"repo-a"}});
    expect(list.queryByText("Beta session")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Repository filter"),{target:{value:""}});
    fireEvent.change(screen.getByLabelText("Model filter"),{target:{value:"gpt-b"}});
    expect(list.queryByText("Alpha session")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Model filter"),{target:{value:""}});
    fireEvent.change(screen.getByLabelText("Date filter"),{target:{value:"2026-01-02"}});
    expect(screen.getByText(/No sessions match/)).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Date filter"),{target:{value:""}});
    for(const label of ["100k+ tokens","Failures","Findings"])fireEvent.click(screen.getByText(label));
    expect(list.getAllByRole("listitem")).toHaveLength(1);
    expect(list.getByText("2 failed")).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Source filter"),{target:{value:"claude"}});
    expect(screen.getByRole("button",{name:/Hide filters/})).toHaveTextContent("4");
  });

  it("separates sessions started by the user, delegated agents and internal reviews",()=>{
    const agent:Session={...second,id:"agent",title:"Claude Code subagent",source:"claude_code",thread_source:"subagent",surface:"subagent"};
    const review:Session={...second,id:"review",title:"Auto-review approval assessment",session_kind:"internal_review",thread_source:"guardian_review"};
    library([first,agent,review]);
    const list=within(screen.getByRole("list",{name:"Sessions"}));
    expect(list.getByText("Alpha session")).toBeInTheDocument();
    expect(list.getByText("you")).toBeInTheDocument();
    expect(list.queryByText("Claude Code subagent")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Agents"}));
    expect(list.getByText("Claude Code subagent")).toBeInTheDocument();
    expect(list.getByText("agent")).toBeInTheDocument();
    expect(list.queryByText("Alpha session")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Reviews"}));
    expect(list.getByText("Auto-review approval assessment")).toBeInTheDocument();
    expect(list.getByText("review")).toBeInTheDocument();
    fireEvent.click(within(screen.getByRole("group",{name:"Filter by starter"})).getByRole("button",{name:"All"}));
    expect(list.getAllByRole("listitem")).toHaveLength(3);
  });

  it("groups by recency and routes drawer, page and import actions",()=>{
    const old:Session={...second,id:"old",title:"Old session",ended_at:"2025-11-01T00:00:00Z"};
    const recent:Session={...first,started_at:"2026-01-01T11:58:00Z",ended_at:new Date(now).toISOString()};
    const props=library([recent,old]);
    expect(screen.getByText("Today",{selector:".list-label"})).toBeInTheDocument();
    expect(screen.getByText("Earlier")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Close session library"}));
    expect(props.onClose).toHaveBeenCalled();
    fireEvent.click(within(screen.getByRole("navigation",{name:"Workspace pages"})).getByRole("button",{name:/Analytics/}));
    expect(props.onPage).toHaveBeenCalledWith("analytics");
    for(const button of screen.getAllByRole("button",{name:/Import conversations/}))fireEvent.click(button);
    expect(props.onImport).toHaveBeenCalledTimes(2);
  });

  it("explains an empty library",()=>{
    library([]);
    expect(screen.getByText(/No conversations imported yet/)).toBeInTheDocument();
  });
});

describe("application rail",()=>{
  it("marks the current page, navigates without reloading, and toggles theme",()=>{
    const onPage=vi.fn(),onTheme=vi.fn();
    const {container,rerender}=render(<AppRail page="analytics" theme="light" onPage={onPage} onTheme={onTheme}/>);
    expect(container.querySelector('svg[data-mark="layered-sift"]')).toBeInTheDocument();
    expect(screen.getByRole("link",{name:"Analytics"})).toHaveAttribute("aria-current","page");
    fireEvent.click(screen.getByRole("link",{name:"Connections"}));
    expect(onPage).toHaveBeenCalledWith("connections");
    fireEvent.click(screen.getByRole("link",{name:"Diagnostics"}));
    expect(onPage).toHaveBeenCalledWith("diagnostics");
    fireEvent.click(screen.getByRole("button",{name:"Use dark theme"}));
    expect(onTheme).toHaveBeenCalled();
    rerender(<AppRail page="diagnostics" theme="dark" onPage={onPage} onTheme={onTheme}/>);
    expect(screen.getByRole("button",{name:"Use light theme"})).toBeInTheDocument();
    expect(screen.getByRole("link",{name:"Diagnostics"})).toHaveAttribute("aria-current","page");
  });
});

describe("top bar and status bar",()=>{
  it("shows session controls and the live support state only on the sessions page",()=>{
    const handlers={onMenu:vi.fn(),onCommand:vi.fn(),onFocus:vi.fn(),onInspector:vi.fn(),onSupport:vi.fn()};
    const {container,rerender}=render(<TopBar page="sessions" place="checkout" focus inspectorOpen={false} support="connected" canShare {...handlers}/>);
    expect(screen.getByText("checkout")).toBeInTheDocument();
    expect(container.querySelector(".top-actions .sep")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Exit focus mode"}));
    fireEvent.click(screen.getByRole("button",{name:"Show inspector"}));
    fireEvent.click(screen.getByRole("button",{name:"Support: supporter connected"}));
    fireEvent.click(screen.getByRole("button",{name:"Open command palette"}));
    fireEvent.click(screen.getByRole("button",{name:"Open session library"}));
    for(const handler of Object.values(handlers))expect(handler).toHaveBeenCalledOnce();
    rerender(<TopBar page="sessions" focus={false} inspectorOpen support="waiting" canShare {...handlers}/>);
    expect(screen.getByRole("button",{name:"Support: code waiting"})).toHaveTextContent("Code active");
    expect(screen.getByRole("button",{name:"Hide inspector"})).toHaveAttribute("aria-pressed","true");
    rerender(<TopBar page="compare" focus={false} inspectorOpen={false} support="none" canShare {...handlers}/>);
    expect(screen.getByText("Compare")).toBeInTheDocument();
    expect(screen.queryByRole("button",{name:/Support/})).not.toBeInTheDocument();
    expect(screen.queryByRole("button",{name:/focus mode/})).not.toBeInTheDocument();
    // The separator divides search from session controls, so it goes with them.
    expect(container.querySelector(".top-actions .sep")).not.toBeInTheDocument();
  });

  it("reports agent reachability and opens diagnostics",()=>{
    const onDiagnostics=vi.fn();
    const {rerender}=render(<StatusBar online location="Local library / 2 sessions" refreshedAt="2026-01-01T10:00:00Z" onDiagnostics={onDiagnostics}/>);
    expect(screen.getByText("Local agent connected")).toBeInTheDocument();
    expect(screen.getByText(/Refreshed/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Diagnostics/}));
    expect(onDiagnostics).toHaveBeenCalled();
    rerender(<StatusBar online={false} location="Analytics" onDiagnostics={onDiagnostics}/>);
    expect(screen.getByText("Local agent unreachable")).toBeInTheDocument();
  });
});
