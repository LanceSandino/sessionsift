/** @module DialogsTest @description Covers native modal dialogs, the share and import dialogs, and the command palette. */
import "@testing-library/jest-dom/vitest";
import React from "react";
import {cleanup,fireEvent,render,screen,waitFor,within} from "@testing-library/react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {CommandPalette,matchCommands,type Command} from "./CommandPalette";
import {Dialog} from "./Dialog";
import {ImportDialog} from "./ImportDialog";
import {SessionInvite} from "./SessionInvite";
import {ShareDialog} from "./ShareDialog";
import {first,json} from "./testFixtures";

afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks()});

describe("dialog",()=>{
  it("opens with the attribute fallback, mounts content only while open, and closes by button, Escape and backdrop",()=>{
    const onClose=vi.fn();
    const {rerender,container}=render(<Dialog open={false} title="Title" onClose={onClose}><p>Body</p></Dialog>);
    const dialog=container.querySelector("dialog")!;
    expect(dialog).not.toHaveAttribute("open");
    expect(screen.queryByText("Body")).not.toBeInTheDocument();
    rerender(<Dialog open title="Title" wide onClose={onClose}><p>Body</p></Dialog>);
    expect(dialog).toHaveAttribute("open");
    expect(dialog).toHaveClass("wide");
    expect(screen.getByRole("heading",{name:"Title"})).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Close dialog"}));
    fireEvent.keyDown(dialog,{key:"Escape"});
    fireEvent.keyDown(dialog,{key:"a"});
    vi.spyOn(dialog,"getBoundingClientRect").mockReturnValue({left:10,right:100,top:10,bottom:100,width:90,height:90,x:10,y:10,toJSON:()=>({})});
    fireEvent.click(dialog,{clientX:50,clientY:50});
    fireEvent.click(screen.getByText("Body"));
    fireEvent.click(dialog,{clientX:5,clientY:50});
    dialog.dispatchEvent(new Event("close"));
    expect(onClose).toHaveBeenCalledTimes(4);
    rerender(<Dialog open={false} title="Title" onClose={onClose}><p>Body</p></Dialog>);
    expect(dialog).not.toHaveAttribute("open");
  });

  it("uses native modal methods where the engine provides them",()=>{
    const showModal=vi.fn(function(this:HTMLDialogElement){this.setAttribute("open","")});
    const close=vi.fn(function(this:HTMLDialogElement){this.removeAttribute("open")});
    Object.defineProperty(HTMLDialogElement.prototype,"showModal",{configurable:true,value:showModal});
    Object.defineProperty(HTMLDialogElement.prototype,"close",{configurable:true,value:close});
    const onClose=vi.fn();
    const {rerender,container}=render(<Dialog open title="Native" onClose={onClose}>x</Dialog>);
    expect(showModal).toHaveBeenCalled();
    fireEvent.keyDown(container.querySelector("dialog")!,{key:"Escape"});
    expect(onClose).not.toHaveBeenCalled();
    rerender(<Dialog open={false} title="Native" onClose={onClose}>x</Dialog>);
    expect(close).toHaveBeenCalled();
    delete (HTMLDialogElement.prototype as {showModal?:unknown}).showModal;
    delete (HTMLDialogElement.prototype as {close?:unknown}).close;
  });
});

describe("share and import dialogs",()=>{
  it("offers a support code or an email invitation for the open conversation only",async()=>{
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL)=>String(input).endsWith("/bridge")?json({connected:false,device_id:"d"}):json([])));
    const onOpenConnections=vi.fn();
    const {rerender}=render(<ShareDialog open session={first} onClose={()=>{}} onOpenConnections={onOpenConnections}/>);
    expect(screen.getByRole("heading",{name:"Share this conversation"})).toBeInTheDocument();
    fireEvent.click(await screen.findByRole("button",{name:/Open Connections/}));
    expect(onOpenConnections).toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button",{name:"Email invitation"}));
    expect(screen.getByRole("button",{name:"Email invitation"})).toHaveAttribute("aria-pressed","true");
    expect(screen.getByLabelText("Invitee email")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Support code"}));
    expect(screen.getByRole("region",{name:"Remote support"})).toBeInTheDocument();
    rerender(<ShareDialog open onClose={()=>{}} onOpenConnections={onOpenConnections}/>);
    expect(screen.queryByRole("heading",{name:"Share this conversation"})).not.toBeInTheDocument();
  });

  it("explains how each source reaches the library",()=>{
    const onClose=vi.fn();
    render(<ImportDialog open onClose={onClose}/>);
    for(const source of ["Claude Code","Codex","Claude.ai","ChatGPT"])expect(screen.getByText(source)).toBeInTheDocument();
    expect(screen.getByText("sessionsift import-chatgpt <export.zip>")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Back to the workspace"}));
    expect(onClose).toHaveBeenCalled();
  });
});

describe("email invitations",()=>{
  it("creates an email-bound invitation for the selected session and copies its link",async()=>{
    const invitation={id:"invite_1",device_id:"device",owner:"owner@example.com",invitee:"reviewer@example.com",session_id:"a",role:"comment",operations:["session.get"],created_at:"2026-01-01T00:00:00Z",expires_at:"2026-01-02T00:00:00Z",accept_url:"https://sessionsift.example/invite/accept/secret"};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>init?.method?json(invitation,201):json([])));
    const writeText=vi.fn(async()=>{});
    Object.defineProperty(navigator,"clipboard",{configurable:true,value:{writeText}});
    render(<SessionInvite sessionId="a" sessionTitle="Alpha session"/>);
    fireEvent.change(screen.getByLabelText("Invitee email"),{target:{value:"reviewer@example.com"}});
    fireEvent.change(screen.getByLabelText("Invitation role"),{target:{value:"comment"}});
    fireEvent.change(screen.getByLabelText("Invitation expiry"),{target:{value:"168"}});
    fireEvent.click(screen.getByRole("button",{name:"Create private link"}));
    expect(await screen.findByText("Link ready for reviewer@example.com")).toBeInTheDocument();
    expect(screen.getByText(/open it directly in Safari or Chrome/i)).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith("/api/v1/remote/invitations",expect.objectContaining({method:"POST",body:expect.stringContaining("\"duration_hours\":168")}));
    fireEvent.click(screen.getByRole("button",{name:/Copy link/}));
    expect(writeText).toHaveBeenCalledWith("https://sessionsift.example/invite/accept/secret");
  });

  it("lists, revokes and reports invitation errors for the selected session only",async()=>{
    const active={id:"invite_existing",device_id:"device",owner:"owner@example.com",invitee:"reviewer@example.com",session_id:"a",role:"view",operations:["session.get"],created_at:"2026-01-01T00:00:00Z",expires_at:"2099-01-02T00:00:00Z",last_viewed_at:"2026-01-01T00:05:00Z",accepted_at:"2026-01-01T00:01:00Z"};
    const expired={...active,id:"old",invitee:"old@example.com",expires_at:"2020-01-01T00:00:00Z",accepted_at:undefined,last_viewed_at:undefined};
    const elsewhere={...active,id:"other",session_id:"b",invitee:"other@example.com"};
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>init?.method?json({revoked:true}):json([active,expired,elsewhere])));
    render(<SessionInvite sessionId="a" sessionTitle="Alpha session"/>);
    expect(await screen.findByText(/view · accepted/)).toBeInTheDocument();
    expect(screen.getByText(/not accepted · expired/)).toBeInTheDocument();
    expect(screen.queryByText("other@example.com")).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button",{name:"Revoke invitation for reviewer@example.com"}));
    await waitFor(()=>expect(fetch).toHaveBeenCalledWith("/api/v1/remote/invitations/revoke",expect.objectContaining({method:"POST",body:expect.stringContaining("invite_existing")})));
    expect(await screen.findByText(/view · accepted · revoked/)).toBeInTheDocument();
    cleanup();
    vi.stubGlobal("fetch",vi.fn((input:RequestInfo|URL,init?:RequestInit)=>init?.method?json({error:"hosted invitations are not configured"},400):Promise.reject(new Error("offline"))));
    render(<SessionInvite sessionId="a" sessionTitle="Alpha session"/>);
    expect(await screen.findByRole("alert")).toHaveTextContent("offline");
    fireEvent.change(screen.getByLabelText("Invitee email"),{target:{value:"x@example.com"}});
    fireEvent.click(screen.getByRole("button",{name:"Create private link"}));
    await waitFor(()=>expect(screen.getByRole("alert")).toHaveTextContent("hosted invitations are not configured"));
  });
});

describe("command palette",()=>{
  const commands=(run=vi.fn()):Command[]=>[
    ...Array.from({length:8},(_,index):Command=>({id:`s${index}`,label:`Session ${index}`,icon:"chat",kind:"session",run})),
    {id:"v",label:"Timeline",icon:"timeline",kind:"view",run},
    {id:"p",label:"Analytics",icon:"chart",kind:"page",run},
    {id:"a",label:"Toggle focus mode",icon:"focus",kind:"action",run},
  ];

  it("matches every word, limits unqueried sessions, and keeps other commands",()=>{
    expect(matchCommands(commands(),"").map(command=>command.id)).toEqual(["s0","s1","s2","s3","s4","s5","v","p","a"]);
    expect(matchCommands(commands(),"session 7").map(command=>command.id)).toEqual(["s7"]);
    expect(matchCommands(commands(),"  FOCUS  mode ").map(command=>command.id)).toEqual(["a"]);
    expect(matchCommands(commands(),"nothing")).toEqual([]);
  });

  it("navigates results with arrow keys and runs the active command",()=>{
    const run=vi.fn();const onClose=vi.fn();
    vi.spyOn(window,"requestAnimationFrame").mockImplementation(callback=>{callback(0);return 0});
    Element.prototype.scrollIntoView=vi.fn();
    const {rerender}=render(<CommandPalette open commands={commands(run)} onClose={onClose}/>);
    const input=screen.getByRole("combobox",{name:"Search commands and sessions"});
    expect(input).toHaveFocus();
    fireEvent.change(input,{target:{value:"t"}});
    const options=()=>within(screen.getByRole("listbox",{name:"Results"})).getAllByRole("option");
    expect(options()[0]).toHaveAttribute("aria-selected","true");
    fireEvent.keyDown(input,{key:"ArrowUp"});
    expect(options().at(-1)).toHaveAttribute("aria-selected","true");
    fireEvent.keyDown(input,{key:"ArrowDown"});
    expect(options()[0]).toHaveAttribute("aria-selected","true");
    fireEvent.mouseEnter(options()[1]);
    fireEvent.keyDown(input,{key:"Enter"});
    expect(onClose).toHaveBeenCalledOnce();
    expect(run).toHaveBeenCalledOnce();
    fireEvent.click(options()[0]);
    expect(run).toHaveBeenCalledTimes(2);
    fireEvent.change(input,{target:{value:"zzz"}});
    expect(screen.getByText("No matching session or command.")).toBeInTheDocument();
    fireEvent.keyDown(input,{key:"ArrowDown"});
    fireEvent.keyDown(input,{key:"Enter"});
    fireEvent.keyDown(input,{key:"x"});
    expect(run).toHaveBeenCalledTimes(2);
    rerender(<CommandPalette open={false} commands={commands(run)} onClose={onClose}/>);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });
});
