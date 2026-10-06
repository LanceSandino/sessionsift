/** @module ActivityCalloutsTest @description Regression anchors for evidence-qualified protocol, Blueprint, memory and file-read labels. */
import {describe,expect,it} from "vitest";
import {activityCallouts,summarizeActivity} from "./activityCallouts";
import type {ToolCall} from "./types";

const read=(overrides:Partial<ToolCall>):ToolCall=>({id:"read",timestamp:"2026-01-01T00:00:00Z",tool_name:"read_file",tool_category:"read",success:true,...overrides});

describe("activity callouts",()=>{
  it("names each observed protocol file instead of making a generic protocol claim",()=>{
    const call=read({arguments_redacted:`sed -n '1,260p' /skills/protocols/MSP.instructions.md /skills/protocols/TICP.instructions.md`});
    expect(activityCallouts(call).map(item=>item.label)).toEqual(["MSP protocol read","TICP protocol read"]);
  });

  it("distinguishes Blueprint, memory and ordinary file reads",()=>{
    expect(activityCallouts(read({target_path_display:"docs/Blueprint.md"})).map(item=>item.label)).toEqual(["Blueprint read"]);
    expect(activityCallouts(read({arguments_redacted:`{"path":"~/.codex/memories/MEMORY.md"}`})).map(item=>item.label)).toEqual(["Memory read"]);
    expect(activityCallouts(read({target_path_display:"src/auth.ts"})).map(item=>item.label)).toEqual(["File read"]);
  });

  it("does not infer a read from a search or prose mention",()=>{
    expect(activityCallouts({...read({}),tool_category:"search",arguments_redacted:"rg MEMORY.md"})).toEqual([]);
  });

  it("summarizes unique protocol names and observed read counts",()=>{
    const calls=[
      read({id:"msp-1",target_path_display:"protocols/MSP.instructions.md"}),
      read({id:"msp-2",arguments_redacted:"cat protocols/MSP.instructions.md"}),
      read({id:"blueprint",target_path_display:"docs/Blueprint.md"}),
      read({id:"memory",target_path_display:"memories/MEMORY.md"}),
      read({id:"file",target_path_display:"src/auth.ts"}),
    ];
    expect(summarizeActivity(calls)).toEqual({protocols:["MSP"],blueprintReads:1,memoryReads:1,fileReads:5});
  });
});
