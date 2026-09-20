/** @module PrivateBetaTests @description Checks intentional sharing and count-only support exports. */
import React from "react";
import {afterEach,describe,expect,it,vi} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import {DiagnosticExport,diagnosticReport} from "./DiagnosticExport";
import {SessionInvite} from "./SessionInvite";
import type {Session} from "../types";
afterEach(()=>{cleanup();vi.restoreAllMocks();vi.unstubAllGlobals()});
const session:Session={id:"private-id",title:"private title",started_at:"2026-01-01",ended_at:"2026-01-01",status:"complete",archived:false,warnings:["private warning secret"],turns:[{id:"t",sequence:1,started_at:"2026-01-01",ended_at:"2026-01-01",user_prompt_redacted:"Visible prompt",input_tokens:0,cached_input_tokens:0,output_tokens:0,reasoning_tokens:0}]};
describe("private beta controls",()=>{
 it("exports only allowlisted counts, with an explicit preview",()=>{
  const json=JSON.stringify(diagnosticReport([session]));expect(json).not.toContain("private-id");expect(json).not.toContain("private title");expect(json).not.toContain("private warning secret");expect(json).not.toContain("Visible prompt");
  render(<DiagnosticExport sessions={[session]}/>);expect(screen.queryByText(/schema/)).not.toBeInTheDocument();fireEvent.click(screen.getByRole("button",{name:"Preview safe report"}));expect(screen.getByText(/sessionsift.support.v1/)).toBeInTheDocument();
  const create=vi.fn(()=>"blob:test");const revoke=vi.fn();Object.defineProperty(URL,"createObjectURL",{configurable:true,value:create});Object.defineProperty(URL,"revokeObjectURL",{configurable:true,value:revoke});vi.spyOn(HTMLAnchorElement.prototype,"click").mockImplementation(()=>{});fireEvent.click(screen.getByRole("button",{name:"Download JSON"}));expect(create).toHaveBeenCalled();expect(revoke).toHaveBeenCalledWith("blob:test");
 });
 it("requires sharing acknowledgment before a private link can be created",()=>{
  vi.stubGlobal("fetch",vi.fn(()=>Promise.resolve(new Response("[]"))));render(<SessionInvite session={session} sessionId={session.id} sessionTitle={session.title}/>);
  fireEvent.change(screen.getByLabelText("Invitee email"),{target:{value:"friend@example.com"}});expect(screen.getByRole("button",{name:"Create private link"})).toBeDisabled();expect(screen.getByText("Visible prompt")).toBeInTheDocument();fireEvent.click(screen.getByRole("checkbox"));expect(screen.getByRole("button",{name:"Create private link"})).toBeEnabled();
 });
});
