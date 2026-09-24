/** @module ReviewWorkspaceTest @description Covers review decisions, the review composer, and the composed session workspace with its tabs and inspector wiring. */
import "@testing-library/jest-dom/vitest";
import React,{createRef} from "react";
import {cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import type {Review} from "../types";
import type {Collaboration} from "../useCollaboration";
import {ReviewComposer} from "./ReviewComposer";
import {ReviewView} from "./ReviewView";
import {SessionWorkspace} from "./SessionWorkspace";
import {first,turn,workedSession} from "./testFixtures";

afterEach(()=>{cleanup();vi.restoreAllMocks()});

function collaboration(overrides:Partial<Collaboration>={}):Collaboration{
  return {annotations:[],reviews:[],error:"",addNote:vi.fn(async()=>{}),recordDecision:vi.fn(async()=>{}),...overrides};
}

describe("review lens",()=>{
  it("records a decision with the reviewer's note or a plain fallback, and shows history newest first",async()=>{
    const reviews:Review[]=[{id:"r1",session_id:"worked",reviewer:"helper@example.com",status:"approved",summary:"Looked fine",created_at:"2026-01-01T00:05:00Z"},{id:"r2",session_id:"worked",reviewer:"local owner",status:"changes_requested",summary:"Check Remove()",created_at:"2026-01-01T00:06:00Z"}];
    const collab=collaboration({reviews});const onToast=vi.fn();
    render(<ReviewView session={workedSession()} collaboration={collab} onToast={onToast}/>);
    const decision=within(screen.getByRole("region",{name:"Review decision"}));
    expect(decision.getByRole("heading",{name:"Needs follow-up"})).toBeInTheDocument();
    expect(decision.getByText(/2 prompts, 5 tool calls, 1 failed/)).toBeInTheDocument();
    expect(decision.getByRole("button",{name:"Needs follow-up"})).toHaveClass("primary");
    const history=screen.getAllByRole("article");
    expect(history[0]).toHaveTextContent("Check Remove()");
    expect(history[1]).toHaveTextContent("Reviewed");
    fireEvent.change(decision.getByLabelText(/Decision note/),{target:{value:"  Verified the race test  "}});
    fireEvent.click(decision.getByRole("button",{name:"Mark reviewed"}));
    await waitFor(()=>expect(collab.recordDecision).toHaveBeenCalledWith("reviewed","Verified the race test"));
    expect(onToast).toHaveBeenCalledWith(expect.stringContaining("Decision recorded"));
    fireEvent.click(decision.getByRole("button",{name:"Needs follow-up"}));
    await waitFor(()=>expect(collab.recordDecision).toHaveBeenLastCalledWith("changes_requested","Follow-up requested."));
  });

  it("starts ready for review, shows notes, and surfaces a failed save",async()=>{
    const collab=collaboration({error:"Local API returned 500",recordDecision:vi.fn(async()=>{throw new Error("nope")}),annotations:[{id:"n",session_id:"a",author:"local owner",body:"Note body",created_at:"2026-01-01T00:00:00Z"}]});
    const onToast=vi.fn();
    render(<ReviewView session={{...first,metrics:undefined}} collaboration={collab} onToast={onToast}/>);
    expect(screen.getByRole("heading",{name:"Awaiting your review"})).toBeInTheDocument();
    expect(screen.getByText("No decision")).toBeInTheDocument();
    expect(screen.getByText("1 prompts, 1 tool calls. Review the evidence",{exact:false})).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Local API returned 500");
    expect(screen.getByText("Note body")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Mark reviewed"}));
    await waitFor(()=>expect(collab.recordDecision).toHaveBeenCalledWith("reviewed","Marked reviewed."));
    expect(onToast).not.toHaveBeenCalled();
  });
});

describe("review composer",()=>{
  it("attaches notes to a prompt, submits with Ctrl+Enter, and never sends to the source agent",async()=>{
    const onSubmit=vi.fn(async()=>{});const onDraft=vi.fn();const onTarget=vi.fn();
    const textarea=createRef<HTMLTextAreaElement>();
    const {rerender}=render(<ReviewComposer turns={[turn]} sourceLabel="Claude Code" target="" draft="" textarea={textarea} onTarget={onTarget} onDraft={onDraft} onSubmit={onSubmit}/>);
    expect(screen.getByText("Annotation only. Not sent to Claude Code.")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:/Add note/}));
    expect(screen.getByRole("alert")).toHaveTextContent("Write a review note first.");
    expect(onSubmit).not.toHaveBeenCalled();
    fireEvent.change(screen.getByLabelText("Attach note to"),{target:{value:"t1"}});
    expect(onTarget).toHaveBeenCalledWith("t1");
    fireEvent.change(screen.getByLabelText("Review note text"),{target:{value:"Check this"}});
    expect(onDraft).toHaveBeenCalledWith("Check this");
    rerender(<ReviewComposer turns={[turn]} sourceLabel="Claude Code" target="t1" draft=" Check this " textarea={textarea} onTarget={onTarget} onDraft={onDraft} onSubmit={onSubmit}/>);
    fireEvent.keyDown(screen.getByLabelText("Review note text"),{key:"Enter",ctrlKey:true});
    await waitFor(()=>expect(onSubmit).toHaveBeenCalledWith("Check this","t1"));
    await waitFor(()=>expect(onDraft).toHaveBeenLastCalledWith(""));
    fireEvent.keyDown(screen.getByLabelText("Review note text"),{key:"a"});
  });

  it("keeps the draft and reports a failed save",async()=>{
    const onSubmit=vi.fn(async()=>{throw new Error("Local API returned 403")});const onDraft=vi.fn();
    render(<ReviewComposer turns={[]} sourceLabel="Codex" target="" draft="Keep me" textarea={createRef()} onTarget={()=>{}} onDraft={onDraft} onSubmit={onSubmit}/>);
    fireEvent.submit(screen.getByRole("form",{name:"Review note"}));
    expect(await screen.findByRole("alert")).toHaveTextContent("Local API returned 403");
    expect(onSubmit).toHaveBeenCalledWith("Keep me",undefined);
    expect(onDraft).not.toHaveBeenCalledWith("");
  });
});

describe("session workspace",()=>{
  function workspace(overrides:Partial<React.ComponentProps<typeof SessionWorkspace>>={}){
    const props={session:workedSession(),view:"conversation" as const,collaboration:collaboration(),onView:vi.fn(),onRevealInspector:vi.fn(),onDismissOverlay:vi.fn(),onCloseInspector:vi.fn(),onCompare:vi.fn(),onToast:vi.fn(),...overrides};
    const view=render(<SessionWorkspace {...props}/>);
    return {props,...view};
  }

  it("navigates lenses with accessible tabs and keeps one scroll pane",()=>{
    const {props,rerender}=workspace();
    const tabs=screen.getByRole("tablist",{name:"Session views"});
    const conversation=within(tabs).getByRole("tab",{name:/Conversation/});
    expect(conversation).toHaveAttribute("aria-selected","true");
    expect(within(tabs).getByRole("tab",{name:/Tools/})).toHaveTextContent("5");
    const panel=()=>document.getElementById("session-panel");
    expect(panel()).toHaveAttribute("aria-labelledby","view-tab-conversation");
    fireEvent.keyDown(conversation,{key:"ArrowRight"});
    expect(props.onView).toHaveBeenLastCalledWith("timeline");
    fireEvent.keyDown(conversation,{key:"ArrowLeft"});
    expect(props.onView).toHaveBeenLastCalledWith("review");
    fireEvent.keyDown(conversation,{key:"End"});
    fireEvent.keyDown(conversation,{key:"Home"});
    expect(props.onView).toHaveBeenLastCalledWith("conversation");
    fireEvent.keyDown(conversation,{key:"x"});
    for(const view of ["timeline","tools","review"] as const){
      rerender(<SessionWorkspace {...props} view={view}/>);
      expect(panel()).toHaveAttribute("aria-labelledby",`view-tab-${view}`);
    }
    expect(screen.getByRole("form",{name:"Review note"})).toBeInTheDocument();
    rerender(<SessionWorkspace {...props} view="tools"/>);
    expect(screen.queryByRole("form",{name:"Review note"})).not.toBeInTheDocument();
  });

  it("toggles the normalized record and returns to the lens when a tab is chosen",()=>{
    const {props}=workspace();
    fireEvent.click(screen.getByRole("button",{name:"Show normalized record"}));
    expect(screen.getByRole("region",{name:"Normalized session record"})).toHaveTextContent('"id": "worked"');
    expect(screen.queryByRole("form",{name:"Review note"})).not.toBeInTheDocument();
    const create=vi.fn(()=>"blob:x");const revoke=vi.fn();
    Object.defineProperty(URL,"createObjectURL",{configurable:true,value:create});Object.defineProperty(URL,"revokeObjectURL",{configurable:true,value:revoke});
    vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(()=>{});
    fireEvent.click(screen.getByRole("button",{name:"Download JSON"}));
    expect(revoke).toHaveBeenCalledWith("blob:x");
    fireEvent.click(screen.getByRole("tab",{name:/Timeline/}));
    expect(props.onView).toHaveBeenCalledWith("timeline");
    expect(screen.getByRole("button",{name:"Show normalized record"})).toHaveAttribute("aria-pressed","false");
  });

  it("opens evidence in the inspector, notes on a prompt, and posts notes through the collaboration store",async()=>{
    const collab=collaboration();
    const {props}=workspace({collaboration:collab});
    fireEvent.click(screen.getByRole("button",{name:"Inspect prompt 1"}));
    expect(props.onRevealInspector).toHaveBeenCalled();
    expect(screen.getByRole("tab",{name:"Evidence"})).toHaveAttribute("aria-selected","true");
    fireEvent.click(screen.getByRole("button",{name:"Add a note on this prompt"}));
    expect(props.onDismissOverlay).toHaveBeenCalled();
    expect(screen.getByLabelText("Attach note to")).toHaveValue("t1");
    await waitFor(()=>expect(screen.getByLabelText("Review note text")).toHaveFocus());
    fireEvent.change(screen.getByLabelText("Review note text"),{target:{value:"Confirm Remove()"}});
    fireEvent.click(screen.getByRole("button",{name:/Add note/}));
    await waitFor(()=>expect(collab.addNote).toHaveBeenCalledWith("Confirm Remove()","t1"));
    expect(props.onToast).toHaveBeenCalledWith(expect.stringContaining("Nothing was sent"));
    fireEvent.click(screen.getByRole("button",{name:/Open in conversation/}));
    expect(props.onView).toHaveBeenCalledWith("conversation");
    fireEvent.click(screen.getByRole("button",{name:"Close inspector"}));
    expect(props.onCloseInspector).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("tab",{name:"Context"}));
    fireEvent.click(screen.getByRole("button",{name:/Compare with another session/}));
    expect(props.onCompare).toHaveBeenCalled();
  });

  it("moves to the conversation before noting from another lens, and remembers open tool runs",()=>{
    const {props,rerender}=workspace({view:"tools"});
    fireEvent.click(screen.getByRole("row",{name:"Inspect Edit: cart.go"}));
    fireEvent.click(screen.getByRole("button",{name:"Add a review note"}));
    expect(props.onView).toHaveBeenCalledWith("conversation");
    rerender(<SessionWorkspace {...props} view="conversation"/>);
    fireEvent.click(screen.getByText("4 tool calls"));
    expect(screen.getAllByRole("button",{name:/^Inspect Bash/})).toHaveLength(2);
    rerender(<SessionWorkspace {...props} view="timeline"/>);
    rerender(<SessionWorkspace {...props} view="conversation"/>);
    expect(screen.getAllByRole("button",{name:/^Inspect Bash/})).toHaveLength(2);
  });
});
