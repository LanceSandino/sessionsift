/** @module FindingList @description Deterministic findings with their confidence language; each opens its evidence in the inspector. */
import React from "react";
import type {Finding} from "../types";
import type {Evidence} from "../workspace";
import {Icon} from "./Icon";

export function FindingList({findings,limit,onSelect}:{findings:Finding[];limit?:number;onSelect:(evidence:Evidence)=>void}){
  const shown=limit?findings.slice(0,limit):findings;
  if(!findings.length)return <p className="empty">No deterministic findings for this session.</p>;
  return <div>
    {shown.map(finding=><button type="button" key={finding.id} className="finding-row" onClick={()=>onSelect({kind:"finding",id:finding.id})}>
      <Icon name="spark"/><div className="grow"><div className="row between wrap"><strong>{finding.title}</strong><span className={`badge confidence ${finding.confidence==="observed"?"good":finding.confidence==="possible"?"warn":""}`}>{finding.confidence}</span></div><p>{finding.summary}</p></div>
    </button>)}
    {findings.length>shown.length&&<p className="small muted">{findings.length-shown.length} more in the Tools view.</p>}
  </div>;
}
