/** @module SessionEvidenceTest @description Anchors per-prompt runs, derived phases, referenced files and call labels to observed records. */
import {describe,expect,it} from "vitest";
import {callLabel,callStatus,callsByTurn,explorationCounts,lastValidation,phaseOf,referencedFiles,runOutcome,transportLabel,turnAt,workPhases} from "./sessionEvidence";
import type {ToolCall,Turn} from "./types";

const turn=(id:string,sequence:number,start:string):Turn=>({id,sequence,started_at:start,ended_at:start,input_tokens:0,cached_input_tokens:0,output_tokens:0,reasoning_tokens:0});
const call=(id:string,at:string,category:string,extra:Partial<ToolCall>={}):ToolCall=>({id,timestamp:`2026-01-01T00:00:${at}Z`,tool_name:"exec",tool_category:category,success:true,...extra});

describe("session evidence",()=>{
  it("assigns calls to their prompt, or to the latest prompt started by then, in time order",()=>{
    const turns=[turn("b",2,"2026-01-01T00:00:30Z"),turn("a",1,"2026-01-01T00:00:00Z")];
    const runs=callsByTurn({turns,tool_calls:[call("late","40","read",{turn_id:"a"}),call("early","10","read"),call("orphan","35","read",{turn_id:"missing"}),call("first","05","read",{turn_id:"a"}),call("before","00","read")]});
    expect([...runs.keys()]).toEqual(["a","b"]);
    expect(runs.get("a")!.map(value=>value.id)).toEqual(["before","first","early","late"]);
    expect(runs.get("b")!.map(value=>value.id)).toEqual(["orphan"]);
    expect(callsByTurn({turns:[],tool_calls:[call("x","01","read")]}).size).toBe(0);
    expect(callsByTurn({}).size).toBe(0);
    expect(callsByTurn({turns}).get("a")).toEqual([]);
    // A call stamped at the exact moment a prompt starts belongs to that prompt.
    expect(callsByTurn({turns,tool_calls:[call("edge","30","read")]}).get("b")!.map(value=>value.id)).toEqual(["edge"]);
    expect(turnAt([],"2026-01-01T00:00:00Z")).toBeUndefined();
    expect(turnAt(turns.slice().reverse(),"2025-12-31T00:00:00Z")?.id).toBe("a");
  });

  it("maps categories and transports to phases and lists only the phases present, in first-seen order",()=>{
    expect(phaseOf(call("r","01","read"))).toBe("explore");
    expect(phaseOf(call("s","01","search"))).toBe("explore");
    expect(phaseOf(call("e","01","edit"))).toBe("edit");
    expect(phaseOf(call("v","01","validation"))).toBe("validate");
    expect(phaseOf(call("x","01","execute"))).toBe("run");
    expect(phaseOf(call("o","01","other"))).toBeUndefined();
    expect(phaseOf(call("m","01","edit",{tool_transport:"mcp"}))).toBe("external");
    expect(phaseOf(call("p","01","other",{tool_transport:"app"}))).toBe("external");
    const phases=workPhases([call("v2","30","validation"),call("e1","10","edit"),call("v1","20","validation",{success:false}),call("r1","05","read"),call("o","01","other")]);
    expect(phases.map(phase=>[phase.id,phase.label,phase.count,phase.failed,phase.firstCallId])).toEqual([["explore","Explore",1,0,"r1"],["edit","Edit",1,0,"e1"],["validate","Validate",2,1,"v1"]]);
    expect(workPhases([])).toEqual([]);
    const input=[call("m","09","other",{tool_transport:"mcp"}),call("x","02","execute")];
    expect(workPhases(input).map(phase=>phase.label)).toEqual(["Run","MCP & apps"]);
    expect(input.map(value=>value.id)).toEqual(["m","x"]);
  });

  it("ranks edited files first, then by activity, and keeps every call that touched them",()=>{
    const files=referencedFiles([call("1","01","read",{target_path_display:"b.go"}),call("2","02","read",{target_path_display:"b.go"}),call("3","03","read",{target_path_display:"a.go"}),call("4","04","edit",{target_path_display:"c.go"}),call("5","05","read"),call("6","06","read",{target_path_display:"a.go"}),call("7","00","read",{target_path_display:"d.go"})]);
    expect(files.map(file=>[file.path,file.count,file.edited])).toEqual([["c.go",1,true],["a.go",2,false],["b.go",2,false],["d.go",1,false]]);
    expect(files[1].callIds).toEqual(["3","6"]);
    expect(files[1].lastAt).toBe("2026-01-01T00:00:06Z");
    expect(files[2].lastAt).toBe("2026-01-01T00:00:02Z");
    expect(referencedFiles([call("late","09","read",{target_path_display:"z.go"}),call("early","03","read",{target_path_display:"z.go"})])[0].lastAt).toBe("2026-01-01T00:00:09Z");
  });

  it("says a run recovered only when a success follows its last failure",()=>{
    expect(runOutcome([])).toEqual({total:0,failed:0,recovered:false});
    expect(runOutcome([call("a","01","x")])).toEqual({total:1,failed:0,recovered:false});
    expect(runOutcome([call("a","01","x",{success:false}),call("b","02","x")])).toEqual({total:2,failed:1,recovered:true});
    expect(runOutcome([call("a","01","x"),call("b","02","x",{success:false})])).toEqual({total:2,failed:1,recovered:false});
    expect(runOutcome([call("a","01","x",{success:false}),call("b","02","x"),call("c","03","x",{success:false})])).toEqual({total:3,failed:2,recovered:false});
  });

  it("labels calls by command, file, then humanized tool name, and reports status and transport",()=>{
    expect(callLabel({tool_name:"exec",command_signature:"go test ./...",target_path_display:"x.go"})).toBe("go test ./...");
    expect(callLabel({tool_name:"read_file",target_path_display:"x.go"})).toBe("x.go");
    expect(callLabel({tool_name:"mcp__github__create_pull_request"})).toBe("github · create pull request");
    expect(callStatus({success:false,tool_category:"validation"})).toBe("Failed");
    expect(callStatus({success:true,tool_category:"validation"})).toBe("Passed");
    expect(callStatus({success:true,tool_category:"edit"})).toBe("Complete");
    expect(transportLabel({tool_transport:"mcp"})).toBe("MCP");
    expect(transportLabel({tool_transport:"app"})).toBe("App");
    expect(transportLabel({})).toBe("Built-in");
    expect(lastValidation([call("v1","01","validation"),call("e","02","edit"),call("v2","03","validation")])?.id).toBe("v2");
    expect(lastValidation([call("e","02","edit")])).toBeUndefined();
    expect(explorationCounts([call("a","01","read"),call("b","02","search"),call("c","03","edit")])).toEqual({explore:2,total:3});
  });
});
