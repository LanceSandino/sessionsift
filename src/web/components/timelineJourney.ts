/** @module TimelineJourney @description Condenses dense tool and event evidence into one human-readable checkpoint per prompt. */
import type {Event,Session,ToolCall,Turn} from "../types";

export type JourneyNote={id:string;kind:string;text:string;at:string};
export type JourneyCheckpoint={id:string;sequence:number;startedAt:string;endedAt:string;prompt:string;turn:Turn;toolCount:number;mcpCount:number;failedCount:number;categories:Record<string,number>;notes:JourneyNote[]};

export function buildTimelineJourney(session:Session):JourneyCheckpoint[]{
  const checkpoints:JourneyCheckpoint[]=(session.turns||[]).slice().sort((a,b)=>a.started_at.localeCompare(b.started_at)).map(turn=>({id:turn.id,sequence:turn.sequence,startedAt:turn.started_at,endedAt:turn.ended_at,prompt:turn.user_prompt_redacted||"Redacted prompt",turn,toolCount:0,mcpCount:0,failedCount:0,categories:{},notes:[]}));
  const byTurn=new Map<string|undefined,JourneyCheckpoint>(checkpoints.map(checkpoint=>[checkpoint.id,checkpoint]));
  if(session.tool_calls)for(const call of session.tool_calls)addTool(byTurn.get(call.turn_id)||nearestCheckpoint(checkpoints,call.timestamp),call);
  if(session.events)for(const event of session.events){
    if(event.event_type==="prompt"||event.event_type==="tool")continue;
    const checkpoint=byTurn.get(event.turn_id)||nearestCheckpoint(checkpoints,event.timestamp);
    if(checkpoint)checkpoint.notes.push({id:event.id,kind:event.event_subtype==="steering_or_queued"?"STEER / QUEUE":(event.event_subtype||event.event_type).toUpperCase(),text:eventText(event),at:event.timestamp});
  }
  for(const checkpoint of checkpoints)checkpoint.notes.sort((a,b)=>a.at.localeCompare(b.at));
  return checkpoints;
}

function addTool(checkpoint:JourneyCheckpoint|undefined,call:ToolCall){
  if(!checkpoint)return;
  checkpoint.toolCount++;
  if(call.tool_transport==="mcp")checkpoint.mcpCount++;
  if(!call.success)checkpoint.failedCount++;
  const category=call.tool_category||"other";
  checkpoint.categories[category]=(checkpoint.categories[category]||0)+1;
}

function nearestCheckpoint(checkpoints:JourneyCheckpoint[],timestamp:string){
  let nearest:JourneyCheckpoint|undefined;
  for(const checkpoint of checkpoints){
    if(checkpoint.startedAt>timestamp)break;
    nearest=checkpoint;
  }
  return nearest||checkpoints[0];
}

function eventText(event:Event){
  const payload=event.payload_redacted||{};
  return String(payload.text||payload.prompt||payload.name||payload.tool_name||event.event_subtype||event.event_type);
}
