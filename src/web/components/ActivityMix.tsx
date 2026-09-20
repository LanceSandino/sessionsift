/** @module ActivityMix @description Explains the observed balance of exploration and implementation activity. */
import React from "react";
import {percent} from "../format";

export function ActivityMix({explorationRatio}:{explorationRatio:number}){
  return <section className="card contextCard"><div className="sectionTitle"><h3>Observed activity mix</h3><span className="pill">derived</span></div><p>Reading and searching accounted for {percent(explorationRatio)} of tool calls.</p><div className="bar"><span style={{width:`${Math.round(explorationRatio*100)}%`}}/></div><div className="barLegend"><span>Explore</span><span>Implement / validate / other</span></div></section>;
}
