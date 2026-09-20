/** @module UsageMetrics @description Formats observed account usage and model-setting changes without claiming causal attribution. */
import type {Turn} from "./types";

export type ModelSettingPeriod={
  key:string;
  model:string;
  effort:string;
  firstSequence:number;
  lastSequence:number;
  startedAt:string;
  usageDelta:number;
  measuredTurns:number;
};

export function observedUsageDelta(turn:Turn):number|undefined{
  if(turn.rate_limit_used_start===undefined||turn.rate_limit_used_end===undefined)return undefined;
  const delta=turn.rate_limit_used_end-turn.rate_limit_used_start;
  return delta>=0?delta:undefined;
}

export function modelSettingPeriods(turns:Turn[]):ModelSettingPeriod[]{
  const periods:ModelSettingPeriod[]=[];
  for(const turn of turns){
    const model=turn.model||"Model unavailable";
    const effort=turn.reasoning_effort||"Effort unavailable";
    const key=`${model}\u0000${effort}`;
    const delta=observedUsageDelta(turn);
    const previous=periods.at(-1);
    if(previous?.key===key){
      previous.lastSequence=turn.sequence;
      if(delta!==undefined){previous.usageDelta+=delta;previous.measuredTurns++}
      continue;
    }
    periods.push({key,model,effort,firstSequence:turn.sequence,lastSequence:turn.sequence,startedAt:turn.started_at,usageDelta:delta||0,measuredTurns:delta===undefined?0:1});
  }
  return periods;
}

export function usageDeltaLabel(value:number):string{
  return `+${new Intl.NumberFormat(undefined,{maximumFractionDigits:1}).format(value)} points`;
}
