/** @module TimelineJourneyTest @description Verifies human-scale prompt checkpoints derived from dense session evidence. */
import {describe,expect,it} from "vitest";
import type {Session,Turn} from "../types";
import {buildTimelineJourney,journeyEntries,type JourneyCheckpoint} from "./timelineJourney";

const turn=(id:string,sequence:number,start:string,end:string):Turn=>({id,sequence,started_at:start,ended_at:end,user_prompt_redacted:`Prompt ${sequence}`,input_tokens:10,cached_input_tokens:5,output_tokens:2,reasoning_tokens:0});

describe("timeline journey",()=>{
  it("groups dense tool evidence into prompt checkpoints",()=>{
    const session:Session={id:"session",title:"Journey",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:05:00Z",status:"completed",archived:false,turns:[turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:02:00Z"),turn("two",2,"2026-01-01T00:03:00Z","2026-01-01T00:05:00Z")],tool_calls:[
      {id:"read",turn_id:"one",timestamp:"2026-01-01T00:00:10Z",tool_name:"read_file",tool_category:"read",success:true},
      {id:"edit",turn_id:"one",timestamp:"2026-01-01T00:00:20Z",tool_name:"apply_patch",tool_category:"edit",tool_transport:"mcp",tool_server:"files",success:true},
      {id:"fail",turn_id:"two",timestamp:"2026-01-01T00:03:20Z",tool_name:"test",tool_category:"validation",success:false},
    ],events:[
      {id:"steer",turn_id:"one",timestamp:"2026-01-01T00:01:00Z",event_type:"message",event_subtype:"steering_or_queued",payload_redacted:{text:"Also verify mobile"}},
      {id:"compact",turn_id:"two",timestamp:"2026-01-01T00:04:00Z",event_type:"compaction",payload_redacted:{name:"Context compacted"}},
    ]};
    const journey=buildTimelineJourney(session);
    expect(journey).toHaveLength(2);
    expect(journey[0]).toMatchObject({sequence:1,toolCount:2,mcpCount:1,failedCount:0,categories:{read:1,edit:1}});
    expect(journey[0].notes).toEqual([{id:"steer",kind:"STEER / QUEUE",text:"Also verify mobile",at:"2026-01-01T00:01:00Z",compaction:false}]);
    expect(journey[0].calls.map(call=>call.id)).toEqual(["read","edit"]);
    expect(journey[1]).toMatchObject({sequence:2,toolCount:1,mcpCount:0,failedCount:1,categories:{validation:1}});
    expect(journey[1].notes[0]).toMatchObject({id:"compact",compaction:true});
  });

  it("attaches unassigned notable events to the nearest preceding prompt",()=>{
    const session:Session={id:"session",title:"Journey",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:05:00Z",status:"completed",archived:false,turns:[turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:02:00Z"),turn("two",2,"2026-01-01T00:03:00Z","2026-01-01T00:05:00Z")],tool_calls:[],events:[{id:"context",timestamp:"2026-01-01T00:03:30Z",event_type:"context",payload_redacted:{name:"Remote workspace"}}]};
    const journey=buildTimelineJourney(session);
    expect(journey[1].notes[0]).toMatchObject({kind:"CONTEXT",text:"Remote workspace"});
  });

  it("sorts prompts without mutating input and honors explicit turn associations",()=>{
    const one=turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:02:00Z");
    const two=turn("two",2,"2026-01-01T00:03:00Z","2026-01-01T00:05:00Z");
    const turns=[two,one];
    const session:Session={id:"session",title:"Journey",started_at:one.started_at,ended_at:two.ended_at,status:"completed",archived:false,turns,tool_calls:[{id:"late",turn_id:"one",timestamp:"2026-01-01T00:04:30Z",tool_name:"exec",tool_category:"execute",success:true}],events:[{id:"late-note",turn_id:"one",timestamp:"2026-01-01T00:04:40Z",event_type:"message",payload_redacted:{text:"Still belongs to prompt one"}}]};
    const journey=buildTimelineJourney(session);
    expect(journey.map(checkpoint=>checkpoint.id)).toEqual(["one","two"]);
    expect(turns.map(value=>value.id)).toEqual(["two","one"]);
    expect(journey[0]).toMatchObject({toolCount:1,notes:[{id:"late-note",text:"Still belongs to prompt one"}]});
    expect(journey[1]).toMatchObject({toolCount:0,notes:[]});
  });

  it("uses fallbacks, skips duplicate raw events, and orders notable notes",()=>{
    const prompt={...turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:02:00Z"),user_prompt_redacted:undefined};
    const session:Session={id:"session",title:"Journey",started_at:prompt.started_at,ended_at:prompt.ended_at,status:"completed",archived:false,turns:[prompt],tool_calls:[
      {id:"other-one",turn_id:"one",timestamp:"2026-01-01T00:00:10Z",tool_name:"unknown",tool_category:"",success:true},
      {id:"other-two",turn_id:"one",timestamp:"2026-01-01T00:00:20Z",tool_name:"unknown",tool_category:"",success:true},
    ],events:[
      {id:"late",turn_id:"one",timestamp:"2026-01-01T00:01:30Z",event_type:"context",payload_redacted:{name:"Later"}},
      {id:"prompt-event",turn_id:"one",timestamp:"2026-01-01T00:00:05Z",event_type:"prompt",payload_redacted:{text:"duplicate"}},
      {id:"tool-event",turn_id:"one",timestamp:"2026-01-01T00:00:06Z",event_type:"tool",payload_redacted:{text:"duplicate"}},
      {id:"early",turn_id:"one",timestamp:"2026-01-01T00:00:30Z",event_type:"context",payload_redacted:{name:"Earlier"}},
    ]};
    const [checkpoint]=buildTimelineJourney(session);
    expect(checkpoint.prompt).toBe("Redacted prompt");
    expect(checkpoint.categories).toEqual({other:2});
    expect(checkpoint.notes.map(note=>note.id)).toEqual(["early","late"]);
  });

  it("assigns timestamp-only evidence at prompt boundaries and tolerates empty sessions",()=>{
    const one=turn("one",1,"2026-01-01T00:01:00Z","2026-01-01T00:02:00Z");
    const two=turn("two",2,"2026-01-01T00:03:00Z","2026-01-01T00:05:00Z");
    const calls=[
      {id:"before",timestamp:"2026-01-01T00:00:30Z",tool_name:"exec",tool_category:"execute",success:true},
      {id:"between",timestamp:"2026-01-01T00:02:30Z",tool_name:"exec",tool_category:"execute",success:true},
      {id:"boundary",timestamp:"2026-01-01T00:03:00Z",tool_name:"exec",tool_category:"execute",success:true},
      {id:"after",timestamp:"2026-01-01T00:06:00Z",tool_name:"exec",tool_category:"execute",success:true},
    ];
    const journey=buildTimelineJourney({id:"session",title:"Journey",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:06:00Z",status:"completed",archived:false,turns:[one,two],tool_calls:calls,events:[]});
    expect(journey.map(checkpoint=>checkpoint.toolCount)).toEqual([2,2]);
    expect(buildTimelineJourney({id:"empty",title:"Empty",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:01:00Z",status:"completed",archived:false,tool_calls:[calls[0]],events:[{id:"event",timestamp:"2026-01-01T00:00:30Z",event_type:"context"}]})).toEqual([]);
    expect(buildTimelineJourney({id:"quiet",title:"Quiet",started_at:one.started_at,ended_at:one.ended_at,status:"completed",archived:false,turns:[one]})).toMatchObject([{toolCount:0,failedCount:0,categories:{},notes:[]}]);
  });

  it("orders each checkpoint's calls by time whatever order the record lists them in",()=>{
    const session:Session={id:"s",title:"t",started_at:"2026-01-01T00:00:00Z",ended_at:"2026-01-01T00:05:00Z",status:"completed",archived:false,turns:[turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:05:00Z")],tool_calls:[
      {id:"late",turn_id:"one",timestamp:"2026-01-01T00:03:00Z",tool_name:"exec",tool_category:"execute",success:true},
      {id:"early",turn_id:"one",timestamp:"2026-01-01T00:01:00Z",tool_name:"exec",tool_category:"execute",success:true},
    ]};
    expect(buildTimelineJourney(session)[0].calls.map(call=>call.id)).toEqual(["early","late"]);
  });

  it("keeps every failed call, then recorded events, then the latest routine calls up to the cap",()=>{
    const call=(id:string,second:number,success=true)=>({id,turn_id:"one",timestamp:`2026-01-01T00:00:${String(second).padStart(2,"0")}Z`,tool_name:"exec",tool_category:"execute",success});
    const checkpoint:JourneyCheckpoint={id:"one",sequence:1,startedAt:"2026-01-01T00:00:00Z",endedAt:"2026-01-01T00:01:00Z",prompt:"p",turn:turn("one",1,"2026-01-01T00:00:00Z","2026-01-01T00:01:00Z"),toolCount:6,mcpCount:0,failedCount:2,categories:{},
      notes:[{id:"n1",kind:"HOOK",text:"a",at:"2026-01-01T00:00:05Z",compaction:false},{id:"n2",kind:"HOOK",text:"b",at:"2026-01-01T00:00:15Z",compaction:false}],
      calls:[call("c1",1),call("f1",2,false),call("c2",3),call("c3",20),call("f2",21,false),call("c4",30)]};
    const ids=(cap:number,filter:"all"|"failed"="all")=>{const result=journeyEntries(checkpoint,filter,cap);return {ids:result.shown.map(entry=>entry.kind==="call"?entry.call.id:entry.note.id),hidden:result.hidden}};
    expect(ids(99)).toEqual({ids:["c1","f1","c2","n1","n2","c3","f2","c4"],hidden:0});
    expect(ids(5)).toEqual({ids:["f1","n1","n2","f2","c4"],hidden:3});
    expect(ids(4)).toEqual({ids:["f1","n1","n2","f2"],hidden:4});
    expect(ids(3)).toEqual({ids:["f1","n2","f2"],hidden:5});
    expect(ids(1)).toEqual({ids:["f1","f2"],hidden:6});
    expect(ids(0)).toEqual({ids:["f1","f2"],hidden:6});
    expect(ids(99,"failed")).toEqual({ids:["f1","f2"],hidden:0});
    expect(journeyEntries(checkpoint,"all",99).shown.map(entry=>entry.kind)).toEqual(["call","call","call","note","note","call","call","call"]);
  });
});
