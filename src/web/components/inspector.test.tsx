/** @module InspectorTest @description Covers the contextual inspector: context sections, each evidence kind, notes and tab navigation. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {cleanup,fireEvent,render,screen,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import type {Annotation,Session} from "../types";
import type {Evidence,InspectorTab} from "../workspace";
import {EvidenceInspector} from "./EvidenceInspector";
import {InspectorContext} from "./InspectorContext";
import {InspectorEvidence} from "./InspectorEvidence";
import {actions,first,metrics,turn,workedSession} from "./testFixtures";

afterEach(cleanup);

describe("context tab",()=>{
  it("reports the latest outcome as the assistant's claim and lists derived and observed context",()=>{
    const acts=actions();const onCompare=vi.fn();
    render(<InspectorContext session={workedSession()} actions={acts} onCompare={onCompare}/>);
    expect(screen.getByRole("heading",{name:"Opened PR #482"})).toBeInTheDocument();
    expect(screen.getByText(/SessionSift did not re-run or verify it/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Read in conversation/}));
    expect(acts.jumpToTurn).toHaveBeenCalledWith("t2");
    const sequence=within(screen.getByLabelText("Work sequence"));
    fireEvent.click(sequence.getByRole("button",{name:/Validate/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
    expect(sequence.getByRole("button",{name:/Validate/})).toHaveClass("has-failure");
    expect(screen.getByText("1 reads / searches of 5 calls")).toBeInTheDocument();
    expect(screen.getByText("+10 points")).toBeInTheDocument();
    expect(screen.getByText(/concurrent chats can contribute/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/cart.go/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"file",path:"cart.go"});
    fireEvent.click(screen.getByRole("button",{name:/A validation failed before passing/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"finding",id:"f1"});
    expect(screen.getAllByText("gpt-6-astra")).toHaveLength(2);
    expect(screen.getByText("1 change observed")).toBeInTheDocument();
    for(const value of ["Codex Desktop","vscode","local (derived)","fix/auth","on-request","managed","restricted"])expect(screen.getByText(value)).toBeInTheDocument();
    expect(screen.getByText("Title renamed in Codex.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Compare with another session/}));
    expect(onCompare).toHaveBeenCalled();
  });

  it("handles sessions without replies, tools, metrics or context",()=>{
    const bare:Session={id:"bare",title:"Bare",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:00:00Z",status:"completed",archived:false,turns:[{...turn,assistant_summary_redacted:undefined}]};
    render(<InspectorContext session={bare} actions={actions()} onCompare={()=>{}}/>);
    expect(screen.getByText("No reply text was recorded for this session.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Work sequence")).not.toBeInTheDocument();
    expect(screen.queryByText("Usage")).not.toBeInTheDocument();
    expect(screen.getByText("0 reads / searches of 0 calls")).toBeInTheDocument();
    expect(screen.getByText("No change observed")).toBeInTheDocument();
  });

  it("caps long file lists",()=>{
    const calls=Array.from({length:10},(_,index)=>({id:`r${index}`,turn_id:"t1",timestamp:"2026-01-01T00:00:10Z",tool_name:"Read",tool_category:"read",success:true,target_path_display:`f${index}.go`}));
    render(<InspectorContext session={{...first,tool_calls:calls,metrics:{...metrics,usage_measured_turns:0}}} actions={actions()} onCompare={()=>{}}/>);
    expect(screen.getByText("2 more files appear in tool evidence.")).toBeInTheDocument();
    expect(screen.getByText("Unavailable")).toBeInTheDocument();
  });
});

describe("evidence tab",()=>{
  const evidence=(value:Evidence|undefined,session:Session=workedSession())=>{const acts=actions();render(<InspectorEvidence session={session} evidence={value} canAnnotate actions={acts}/>);return acts};

  it("invites a selection when nothing is chosen and explains missing evidence",()=>{
    evidence(undefined);
    expect(screen.getByText("Select a tool call, file, prompt or finding.")).toBeInTheDocument();
    cleanup();
    for(const value of [{kind:"tool",id:"x"},{kind:"turn",id:"x"},{kind:"file",path:"x"},{kind:"finding",id:"x"}] as Evidence[]){evidence(value);expect(screen.getByText("This evidence is no longer in the session.")).toBeInTheDocument();cleanup()}
  });

  it("shows a call's provenance, redacted record and failure, and links onward",()=>{
    const acts=evidence({kind:"tool",id:"test-fail"});
    expect(screen.getByText("Built-in tool")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("2 / 5")).toBeInTheDocument();
    expect(screen.getByText("1",{selector:".value"})).toBeInTheDocument();
    expect(screen.getByText(/WARNING: DATA RACE/).closest("pre")).toBeInTheDocument();
    expect(screen.getByText(/This failure stays in the record/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/^01/}));
    expect(acts.jumpToTurn).toHaveBeenCalledWith("t1");
    fireEvent.click(screen.getByRole("button",{name:"Add a review note"}));
    expect(acts.noteOn).toHaveBeenCalledWith("t1");
  });

  it("shows MCP calls with their server and target files, and reports absent records",()=>{
    const session=workedSession();
    session.tool_calls!.push({id:"target",turn_id:"t2",timestamp:"2026-01-01T00:03:40Z",tool_name:"read_file",tool_category:"read",success:true,target_path_display:"cart.go"});
    const acts=evidence({kind:"tool",id:"pr"},session);
    expect(screen.getByText("MCP tool")).toBeInTheDocument();
    expect(screen.getByText("github")).toBeInTheDocument();
    expect(screen.getByText("No arguments were retained for this call.")).toBeInTheDocument();
    expect(screen.getByText("No result preview was retained for this call.")).toBeInTheDocument();
    cleanup();
    evidence({kind:"tool",id:"target"},session);
    fireEvent.click(screen.getByRole("button",{name:"cart.go"}));
    expect(acts.selectEvidence).not.toHaveBeenCalled();
  });

  it("explains a prompt with its tokens, usage, files, calls and findings",()=>{
    const acts=evidence({kind:"turn",id:"t1"});
    expect(screen.getByRole("heading",{name:"Inspect auth"})).toBeInTheDocument();
    expect(screen.getByText("+10 points")).toBeInTheDocument();
    expect(screen.getByText(/Window: 300 minutes/)).toBeInTheDocument();
    expect(screen.getByText("4 · 1 failed")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Open in conversation/}));
    expect(acts.jumpToTurn).toHaveBeenCalledWith("t1");
    fireEvent.click(within(screen.getByText("Files touched").closest("section")!).getByRole("button",{name:/checkout/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"file",path:"checkout"});
    fireEvent.click(screen.getByRole("button",{name:/A validation failed before passing/}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"finding",id:"f1"});
    fireEvent.click(screen.getByRole("button",{name:"Add a note on this prompt"}));
    expect(acts.noteOn).toHaveBeenCalledWith("t1");
    cleanup();
    evidence({kind:"turn",id:"t2"},{...workedSession(),tool_calls:[]});
    expect(screen.getByText("Unreported")).toBeInTheDocument();
    expect(screen.getByText("No tools were associated with this prompt.")).toBeInTheDocument();
  });

  it("lists a file's activity and a finding's evidence without claiming file contents",()=>{
    const acts=evidence({kind:"file",path:"cart.go"});
    expect(screen.getByText("Edited")).toBeInTheDocument();
    expect(screen.getByText(/not file contents or diffs/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Edit: cart.go (Complete)"}));
    expect(acts.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"edit"});
    cleanup();
    const next=evidence({kind:"finding",id:"f1"});
    expect(screen.getByText("failed tool loop")).toBeInTheDocument();
    expect(screen.getByText("event-x")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Bash: go test -race ./..."}));
    expect(next.selectEvidence).toHaveBeenCalledWith({kind:"tool",id:"test-fail"});
    fireEvent.click(screen.getByRole("button",{name:/^01/}));
    expect(next.selectEvidence).toHaveBeenCalledWith({kind:"turn",id:"t1"});
  });

  it("caps long call lists and describes metric-only findings",()=>{
    const calls=Array.from({length:35},(_,index)=>({id:`c${index}`,turn_id:"t1",timestamp:"2026-01-01T00:00:10Z",tool_name:"Read",tool_category:"read",success:true,target_path_display:"x.go"}));
    evidence({kind:"file",path:"x.go"},{...first,tool_calls:calls});
    expect(screen.getByText("5 more calls are listed in the Tools view.")).toBeInTheDocument();
    cleanup();
    evidence({kind:"finding",id:"f"},{...first,findings:[{id:"f",finding_type:"exploration_ratio",severity:"info",confidence:"derived",title:"Ratio",summary:"s",evidence:[],metric:{}}]});
    expect(screen.getByText("Derived from session metrics.")).toBeInTheDocument();
  });
});

describe("inspector shell",()=>{
  it("switches tabs by click and arrow keys, lists notes, and routes footer and close actions",()=>{
    const acts=actions();const onTab=vi.fn();const onClose=vi.fn();
    const notes:Annotation[]=[{id:"n",session_id:"worked",turn_id:"t1",author:"helper",body:"Looks right",created_at:"2026-01-01T00:05:00Z"}];
    const {rerender}=render(<EvidenceInspector session={workedSession()} tab="context" annotations={notes} actions={acts} onTab={onTab} onClose={onClose} onCompare={()=>{}}/>);
    const tabs=screen.getByRole("tablist",{name:"Inspector tabs"});
    expect(within(tabs).getByRole("tab",{name:"Notes 1"})).toBeInTheDocument();
    fireEvent.keyDown(within(tabs).getByRole("tab",{name:"Context"}),{key:"ArrowRight"});
    expect(onTab).toHaveBeenLastCalledWith("evidence");
    fireEvent.keyDown(within(tabs).getByRole("tab",{name:"Context"}),{key:"ArrowLeft"});
    expect(onTab).toHaveBeenLastCalledWith("notes");
    fireEvent.keyDown(within(tabs).getByRole("tab",{name:"Context"}),{key:"End"});
    expect(onTab).toHaveBeenLastCalledWith("notes");
    fireEvent.keyDown(within(tabs).getByRole("tab",{name:"Context"}),{key:"Home"});
    expect(onTab).toHaveBeenLastCalledWith("context");
    fireEvent.keyDown(within(tabs).getByRole("tab",{name:"Context"}),{key:"a"});
    fireEvent.click(within(tabs).getByRole("tab",{name:"Evidence"}));
    expect(onTab).toHaveBeenLastCalledWith("evidence");
    fireEvent.click(screen.getByRole("button",{name:/Review this session/}));
    expect(acts.setView).toHaveBeenCalledWith("review");
    fireEvent.click(screen.getByRole("button",{name:"Close inspector"}));
    expect(onClose).toHaveBeenCalled();
    for(const tab of ["evidence","notes"] as InspectorTab[])rerender(<EvidenceInspector session={workedSession()} tab={tab} evidence={{kind:"tool",id:"edit"}} annotations={notes} actions={acts} onTab={onTab} onClose={onClose} onCompare={()=>{}}/>);
    expect(screen.getByText("Looks right")).toBeInTheDocument();
    expect(screen.getByText("· Prompt 1")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Add a note"}));
    expect(acts.noteOn).toHaveBeenCalledWith();
    rerender(<EvidenceInspector session={workedSession()} tab="notes" annotations={[]} actions={acts} onTab={onTab} onClose={onClose} onCompare={()=>{}}/>);
    expect(screen.getByText("No review notes yet.")).toBeInTheDocument();
  });
});
