/** @module ActivityBadges @description Renders compact evidence-qualified labels for significant recorded reads. */
import React from "react";
import {activityCallouts} from "../activityCallouts";
import type {ToolCall} from "../types";

export function ActivityBadges({call,compact=false}:{call:ToolCall;compact?:boolean}){
  const callouts=activityCallouts(call);
  if(!callouts.length)return null;
  const shown=compact?callouts.slice(0,1):callouts;
  return <span className="activity-badges" aria-label={callouts.map(item=>item.label).join(", ")}>
    {shown.map(item=><span className={`activity-badge ${item.kind}`} key={item.label}>{item.label}</span>)}
    {shown.length<callouts.length&&<span className="activity-badge">+{callouts.length-shown.length}</span>}
  </span>;
}
