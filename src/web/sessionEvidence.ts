/** @module SessionEvidence @description Derives per-prompt tool runs, work phases, referenced files and call labels from observed records only. */
import type {Session,ToolCall,Turn} from "./types";

export type PhaseId="explore"|"edit"|"validate"|"run"|"external";
export type WorkPhase={id:PhaseId;label:string;count:number;firstCallId:string;firstAt:string;failed:number};
export type ReferencedFile={path:string;count:number;edited:boolean;lastAt:string;callIds:string[]};
export type RunOutcome={total:number;failed:number;recovered:boolean};

const phaseLabels:Record<PhaseId,string>={explore:"Explore",edit:"Edit",validate:"Validate",run:"Run",external:"MCP & apps"};

/** Calls that carry no prompt ID belong to the latest prompt that had started by then. */
export function callsByTurn(session:Pick<Session,"turns"|"tool_calls">):Map<string,ToolCall[]>{
  const turns=(session.turns||[]).slice().sort((a,b)=>a.started_at.localeCompare(b.started_at));
  const known=new Set(turns.map(turn=>turn.id));
  const result=new Map<string,ToolCall[]>(turns.map(turn=>[turn.id,[]]));
  for(const call of session.tool_calls||[]){
    const turnID=call.turn_id&&known.has(call.turn_id)?call.turn_id:turnAt(turns,call.timestamp)?.id;
    if(turnID)result.get(turnID)!.push(call);
  }
  for(const calls of result.values())calls.sort((a,b)=>a.timestamp.localeCompare(b.timestamp));
  return result;
}

export function turnAt(turns:Turn[],timestamp:string):Turn|undefined{
  let nearest:Turn|undefined;
  for(const turn of turns){
    if(turn.started_at>timestamp)break;
    nearest=turn;
  }
  return nearest||turns[0];
}

export function phaseOf(call:ToolCall):PhaseId|undefined{
  if(call.tool_transport==="mcp"||call.tool_transport==="app")return "external";
  switch(call.tool_category){
    case "read":case "search":return "explore";
    case "edit":return "edit";
    case "validation":return "validate";
    case "execute":return "run";
  }
  return undefined;
}

/** The phases this session actually contains, in the order they first appeared. */
export function workPhases(calls:ToolCall[]):WorkPhase[]{
  const phases=new Map<PhaseId,WorkPhase>();
  for(const call of calls.slice().sort((a,b)=>a.timestamp.localeCompare(b.timestamp))){
    const id=phaseOf(call);
    if(!id)continue;
    const phase=phases.get(id)||{id,label:phaseLabels[id],count:0,firstCallId:call.id,firstAt:call.timestamp,failed:0};
    phase.count++;
    if(!call.success)phase.failed++;
    phases.set(id,phase);
  }
  // Calls are visited in time order, so the map already holds phases in first-seen order.
  return [...phases.values()];
}

export function referencedFiles(calls:ToolCall[]):ReferencedFile[]{
  const files=new Map<string,ReferencedFile>();
  for(const call of calls){
    const path=call.target_path_display;
    if(!path)continue;
    const file=files.get(path)||{path,count:0,edited:false,lastAt:call.timestamp,callIds:[]};
    file.count++;
    file.edited||=call.tool_category==="edit";
    if(call.timestamp>file.lastAt)file.lastAt=call.timestamp;
    file.callIds.push(call.id);
    files.set(path,file);
  }
  return [...files.values()].sort((a,b)=>Number(b.edited)-Number(a.edited)||b.count-a.count||a.path.localeCompare(b.path));
}

/** A run recovered when a failure was followed by a later successful call. */
export function runOutcome(calls:ToolCall[]):RunOutcome{
  const failed=calls.filter(call=>!call.success).length;
  const lastFailure=calls.map(call=>call.success).lastIndexOf(false);
  return {total:calls.length,failed,recovered:failed>0&&calls.slice(lastFailure+1).some(call=>call.success)};
}

export function lastValidation(calls:ToolCall[]):ToolCall|undefined{
  return calls.filter(call=>call.tool_category==="validation").at(-1);
}

export function transportLabel(call:Pick<ToolCall,"tool_transport">):string{
  return call.tool_transport==="mcp"?"MCP":call.tool_transport==="app"?"App":"Built-in";
}

export function callStatus(call:Pick<ToolCall,"success"|"tool_category">):"Failed"|"Passed"|"Complete"{
  if(!call.success)return "Failed";
  return call.tool_category==="validation"?"Passed":"Complete";
}

/** What a person would call the action: the command, the file, or the tool. */
export function callLabel(call:Pick<ToolCall,"command_signature"|"target_path_display"|"tool_name">):string{
  return call.command_signature||call.target_path_display||call.tool_name.replace(/^mcp__/,"").replaceAll("__"," · ").replaceAll("_"," ");
}

export function explorationCounts(calls:ToolCall[]){
  return {explore:calls.filter(call=>call.tool_category==="read"||call.tool_category==="search").length,total:calls.length};
}
