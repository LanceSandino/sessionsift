/** @module ConversationLensTest @description Covers the conversation, timeline and tools lenses: bounded history, tool runs, evidence selection and filters. */
import "@testing-library/jest-dom/vitest";
import React,{createRef} from "react";
import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {absoluteTime} from "../format";
import type {Annotation,Session,ToolCall,Turn} from "../types";
import {Timeline} from "./Timeline";
import {ToolsView,groupCalls} from "./ToolsView";
import {Transcript} from "./Transcript";
import {actions,first,readCall,turn,workedSession} from "./testFixtures";

afterEach(()=>{cleanup();vi.restoreAllMocks()});

function transcript(session:Session,overrides:Partial<React.ComponentProps<typeof Transcript>>={}){
  const props={session,annotations:[] as Annotation[],viewport:createRef<HTMLElement>(),openRuns:{} as Record<string,boolean>,onRunToggle:vi.fn(),actions:actions(),...overrides};
  const view=render(<Transcript {...props}/>);
  return {props,...view};
}

describe("conversation lens",()=>{
  it("uses chat bubbles with clear speakers, model settings and steering messages",()=>{
    const {container}=transcript(first);
    expect(screen.getByLabelText("You message for prompt 1")).toHaveTextContent("Inspect auth");
    expect(screen.getByLabelText("Codex reply for prompt 1")).toHaveTextContent("I inspected the authentication flow");
    expect(screen.getByText("gpt-6-astra · high effort")).toBeInTheDocument();
    expect(screen.getByLabelText("Steering or queued message")).toHaveTextContent("Also push it");
    expect(screen.getByText("Recorded transcript")).toBeInTheDocument();
    expect(container.querySelector(".user-message")).toBeInTheDocument();
  });

  it("loads long history progressively and mounts earlier prompts when a jump needs them",()=>{
    const turns=Array.from({length:20},(_,index):Turn=>({...turn,id:`t${index+1}`,sequence:index+1,started_at:`2026-01-01T00:${String(index).padStart(2,"0")}:00Z`,user_prompt_redacted:`Transcript prompt ${index+1}`}));
    const scrollIntoView=vi.fn();
    Element.prototype.scrollIntoView=scrollIntoView;
    transcript({...first,turns,tool_calls:[]});
    expect(screen.getByText("Transcript prompt 20")).toBeInTheDocument();
    expect(screen.queryByText("Transcript prompt 15")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Show 5 earlier prompts"}));
    expect(screen.getByText("Transcript prompt 15")).toBeInTheDocument();
    expect(screen.queryByText("Transcript prompt 2")).not.toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("Jump to prompt"),{target:{value:"t2"}});
    expect(screen.getByText("Transcript prompt 2")).toBeInTheDocument();
    expect(scrollIntoView).toHaveBeenCalled();
    scrollIntoView.mockClear();
    fireEvent.change(screen.getByLabelText("Jump to prompt"),{target:{value:"t19"}});
    expect(scrollIntoView).toHaveBeenCalledOnce();
    fireEvent.change(screen.getByLabelText("Jump to prompt"),{target:{value:"missing"}});
    fireEvent.click(screen.getByRole("button",{name:/Latest checkpoint/}));
  });

  it("honours a jump request from the inspector",()=>{
    const turns=Array.from({length:6},(_,index):Turn=>({...turn,id:`t${index+1}`,sequence:index+1,user_prompt_redacted:`Prompt text ${index+1}`}));
    const {rerender,props}=transcript({...first,turns,tool_calls:[]});
    expect(screen.queryByText("Prompt text 1")).not.toBeInTheDocument();
    rerender(<Transcript {...props} jump={{turnID:"t1",nonce:1}}/>);
    expect(screen.getByText("Prompt text 1")).toBeInTheDocument();
  });

  it("summarizes a prompt's tool run, reports recovery and opens each call's evidence",()=>{
    const session=workedSession();
    const {props,rerender}=transcript(session);
    expect(screen.getByText("4 tool calls")).toBeInTheDocument();
    expect(screen.getByText("1 failed → later calls succeeded")).toBeInTheDocument();
    fireEvent.click(screen.getByText("4 tool calls"));
    expect(props.onRunToggle).toHaveBeenCalledWith("t1",true);
    rerender(<Transcript {...props} openRuns={{t1:true}} selected={{kind:"tool",id:"edit"}}/>);
    const failed=screen.getByRole("button",{name:"Inspect Bash: go test -race ./... (Failed)"});
    expect(failed).toHaveClass("failed");
    expect(screen.getByRole("button",{name:/Inspect Edit/})).toHaveClass("selected");
    fireEvent.click(failed);
    expect(props.actions.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
    fireEvent.click(screen.getByRole("button",{name:/Inspect run/}));
    expect(props.actions.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-pass"});
    expect(screen.getByText(/Validation passed · go test -race -count=20/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Edited · inspect activity/}));
    expect(props.actions.selectEvidence).toHaveBeenCalledWith({kind:"file",path:"cart.go"});
    fireEvent.click(screen.getByRole("button",{name:"Inspect prompt 2"}));
    expect(props.actions.selectEvidence).toHaveBeenCalledWith({kind:"turn",id:"t2"});
    expect(screen.getByText(/Context compacted/)).toBeInTheDocument();
    expect(screen.getByText("effort unreported",{exact:false})).toBeInTheDocument();
  });

  it("derives a work-sequence strip and opens the first call of a phase",()=>{
    const {props}=transcript(workedSession());
    const strip=within(screen.getByRole("navigation",{name:"Work sequence"}));
    expect(strip.getAllByRole("button").map(button=>button.textContent)).toEqual(["1Explore1","2Validate2","3Edit1","4MCP & apps1"]);
    fireEvent.click(strip.getByRole("button",{name:/Validate/}));
    expect(props.actions.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
  });

  it("caps long tool runs until asked and flags an unrecovered failure",()=>{
    const calls:ToolCall[]=Array.from({length:30},(_,index)=>({...readCall,id:`r${index}`,timestamp:`2026-01-01T00:00:${String(index).padStart(2,"0")}Z`,success:index!==29}));
    transcript({...first,tool_calls:calls},{openRuns:{t1:true}});
    expect(screen.getAllByRole("button",{name:/Inspect read_file/})).toHaveLength(25);
    expect(screen.getByText("1 failed")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Show all 30 calls"}));
    expect(screen.getAllByRole("button",{name:/Inspect read_file/})).toHaveLength(30);
  });

  it("shows notes beside their prompt and whole-session notes at the end",()=>{
    const notes:Annotation[]=[{id:"n1",session_id:"a",turn_id:"t1",author:"local owner",body:"Check the branch",created_at:"2026-01-01T00:05:00Z"},{id:"n2",session_id:"a",author:"helper",body:"Overall fine",created_at:"2026-01-01T00:06:00Z"},{id:"n3",session_id:"a",turn_id:"gone",author:"helper",body:"Stale target",created_at:"2026-01-01T00:07:00Z"}];
    transcript(first,{annotations:notes});
    expect(screen.getByText("Review note · Prompt 1")).toBeInTheDocument();
    expect(screen.getByText("Review note · Entire session")).toBeInTheDocument();
    expect(screen.getByText("Review note · A prompt no longer in this session")).toBeInTheDocument();
  });

  it("reports sessions without turns or replies",()=>{
    const {rerender,props}=transcript({...first,turns:[],tool_calls:[]});
    expect(screen.getByText("No transcript turns were observed.")).toBeInTheDocument();
    rerender(<Transcript {...props} session={{...first,source:"chatgpt",turns:[{...turn,assistant_summary_redacted:undefined,user_prompt_redacted:undefined,model:undefined}],model:undefined,tool_calls:[]}}/>);
    expect(screen.getByText("No reply text was recorded for this prompt.")).toBeInTheDocument();
    expect(screen.getByText("Imported export")).toBeInTheDocument();
    expect(screen.getByText("Prompt content unavailable.")).toBeInTheDocument();
    expect(screen.getByText(/Model unavailable/)).toBeInTheDocument();
  });
});

describe("timeline lens",()=>{
  const timeline=(session:Session)=>{const acts=actions();render(<Timeline session={session} viewport={createRef<HTMLElement>()} actions={acts}/>);return acts};

  it("summarizes dense tool evidence per prompt instead of raw command cards",()=>{
    const calls=Array.from({length:15},(_,index):ToolCall=>({...readCall,id:`call-${index}`,tool_category:index===14?"validation":"execute",tool_transport:index===13?"mcp":"builtin",success:index!==14}));
    timeline({...first,tool_calls:calls});
    expect(screen.getByText("15 tool calls")).toBeInTheDocument();
    expect(screen.getByText("14 execute")).toBeInTheDocument();
    expect(screen.getByText("1 validation")).toBeInTheDocument();
    expect(screen.getByText("1 MCP")).toBeInTheDocument();
    expect(screen.getByText("1 failed")).toBeInTheDocument();
    expect(screen.getByText("+10 points usage")).toBeInTheDocument();
    expect(screen.getByText(`Prompt checkpoints, tool activity and recorded events. Started ${absoluteTime(first.started_at)}.`)).toBeInTheDocument();
    // One failed call and one recorded event always show; six latest routine calls fill the cap of eight.
    expect(screen.getAllByRole("button",{name:/src\/auth.ts/})).toHaveLength(7);
    expect(screen.getByText("Also push it")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Show all 16 events/}));
    expect(screen.getAllByRole("button",{name:/src\/auth.ts/})).toHaveLength(15);
  });

  it("filters to failed calls and opens prompts and calls as evidence",()=>{
    const acts=timeline(workedSession());
    fireEvent.click(screen.getByRole("button",{name:/Failed calls/}));
    expect(screen.getByRole("button",{name:/Failed calls/})).toHaveAttribute("aria-pressed","true");
    expect(screen.getByText("No failed calls in this checkpoint.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/go test -race \.\/\.\.\./}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
    fireEvent.click(screen.getByRole("button",{name:/Open prompt 1: Inspect auth/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"turn",id:"t1"});
    fireEvent.click(screen.getByRole("button",{name:"All activity"}));
    expect(screen.getByText(/compact_boundary · recorded boundary/)).toBeInTheDocument();
    expect(screen.getByText("Passed")).toBeInTheDocument();
  });

  it("keeps the newest checkpoint window and reveals earlier prompts",()=>{
    const turns=Array.from({length:250},(_,index):Turn=>({...turn,id:`turn-${index+1}`,sequence:index+1,started_at:`2026-01-01T${String(Math.floor(index/60)).padStart(2,"0")}:${String(index%60).padStart(2,"0")}:00Z`,user_prompt_redacted:`Evidence event ${index+1}`}));
    timeline({...first,turns,events:[],tool_calls:[]});
    expect(screen.getByText("Evidence event 250")).toBeInTheDocument();
    expect(screen.queryByText("Evidence event 1")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Show 242 earlier checkpoints"}));
    expect(screen.getByText("Evidence event 1")).toBeInTheDocument();
  });

  it("jumps within the pane and keeps ungrouped raw evidence out of the journey",()=>{
    const later:Turn={...turn,id:"t2",sequence:2,started_at:"2026-01-01T00:02:00Z",user_prompt_redacted:"Second prompt"};
    vi.spyOn(window,"requestAnimationFrame").mockImplementation(callback=>{callback(0);return 0});
    Element.prototype.scrollIntoView=vi.fn();
    timeline({...first,turns:[turn,later]});
    fireEvent.change(screen.getByLabelText("Jump to timeline prompt"),{target:{value:"t1"}});
    expect(Element.prototype.scrollIntoView).toHaveBeenCalled();
    cleanup();
    const events=Array.from({length:201},(_,index)=>({id:`e${index}`,timestamp:"2026-01-01T00:01:00Z",event_type:"hook",event_subtype:"PostToolUse",payload_redacted:{name:`tool ${index}`}}));
    timeline({...first,turns:[],tool_calls:[],events});
    expect(screen.getByText("No prompt checkpoints available.")).toBeInTheDocument();
    expect(screen.queryByText("tool 200")).not.toBeInTheDocument();
  });
});

describe("tools lens",()=>{
  it("calls out named protocols plus Blueprint, memory and file reads from observed tool evidence",()=>{
    const calls:ToolCall[]=[
      {...readCall,id:"protocol",target_path_display:"protocols/MSP.instructions.md",arguments_redacted:`{"path":"/skills/protocols/MSP.instructions.md"}`},
      {...readCall,id:"blueprint",target_path_display:"docs/Blueprint.md"},
      {...readCall,id:"memory",target_path_display:"memories/MEMORY.md"},
      {...readCall,id:"file",target_path_display:"src/auth.ts"},
    ];
    render(<ToolsView session={{...first,tool_calls:calls}} actions={actions()}/>);
    const summary=screen.getByRole("region",{name:"Observed protocol and file reads"});
    expect(summary).toHaveTextContent("MSP");
    expect(summary).toHaveTextContent("1 Blueprint read");
    expect(summary).toHaveTextContent("1 memory read");
    expect(summary).toHaveTextContent("4 file reads total");
    expect(screen.getByRole("row",{name:/Inspect read_file: protocols\/MSP.instructions.md/})).toHaveTextContent("MSP protocol read");
    expect(screen.getByRole("row",{name:/Inspect read_file: docs\/Blueprint.md/})).toHaveTextContent("Blueprint read");
    expect(screen.getByRole("row",{name:/Inspect read_file: memories\/MEMORY.md/})).toHaveTextContent("Memory read");
    expect(screen.getByRole("row",{name:/Inspect read_file: src\/auth.ts/})).toHaveTextContent("File read");
  });

  it("keeps built-in and MCP activity distinct and opens calls by click or keyboard",()=>{
    const acts=actions();
    const session:Session={...workedSession(),tool_calls:[...workedSession().tool_calls!,{id:"m2",turn_id:"t2",timestamp:"2026-01-01T00:03:20Z",tool_name:"mcp__github__create_pull_request",tool_category:"other",tool_transport:"mcp",tool_server:"github",success:false}]};
    render(<ToolsView session={session} selected={{kind:"tool",id:"edit"}} actions={acts}/>);
    expect(screen.getByText("github MCP")).toBeInTheDocument();
    expect(screen.getByText("Failed calls are preserved as evidence",{exact:false})).toBeInTheDocument();
    expect(screen.getByText("1 reads / searches of 6")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("row",{name:"Inspect the latest mcp__github__create_pull_request call"}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"m2"});
    const failed=screen.getByRole("row",{name:"Inspect Bash: go test -race ./..."});
    expect(failed).toHaveTextContent("Failed · exit 1");
    fireEvent.keyDown(failed,{key:"Enter"});
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
    fireEvent.keyDown(failed,{key:"Tab"});
    expect(screen.getByRole("row",{name:"Inspect Edit: cart.go"})).toHaveAttribute("aria-current","true");
    fireEvent.click(screen.getByRole("button",{name:/A validation failed before passing/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"finding",id:"f1"});
  });

  it("windows long call lists and reports empty sessions",()=>{
    const calls=Array.from({length:70},(_,index):ToolCall=>({...readCall,id:`r${index}`,timestamp:`2026-01-01T00:${String(Math.floor(index/60)).padStart(2,"0")}:${String(index%60).padStart(2,"0")}Z`}));
    const {rerender}=render(<ToolsView session={{...first,tool_calls:calls}} actions={actions()}/>);
    expect(screen.getByText("Latest 60 of 70")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Show 10 earlier calls"}));
    expect(screen.getByText("70 in order")).toBeInTheDocument();
    rerender(<ToolsView session={{...first,tool_calls:[],findings:[]}} actions={actions()}/>);
    expect(screen.getByText("No tool calls were observed.")).toBeInTheDocument();
    expect(screen.getByText("No deterministic findings for this session.")).toBeInTheDocument();
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  it("groups calls by transport, server and name with the latest call",()=>{
    const groups=groupCalls([{...readCall,id:"a",timestamp:"2026-01-01T00:00:01Z"},{...readCall,id:"b",timestamp:"2026-01-01T00:00:03Z",success:false},{...readCall,id:"c",tool_transport:"mcp",tool_server:"fs",timestamp:"2026-01-01T00:00:02Z"}]);
    expect(groups.map(group=>[group.key,group.calls,group.failed,group.latestID])).toEqual([["builtin::read_file",2,1,"b"],["mcp:fs:read_file",1,0,"c"]]);
  });
});
