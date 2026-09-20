/** @module MetricCard @description Presents one observable session metric. */
import React from "react";
export function MetricCard({label,value,sub,tone}:{label:string;value:string;sub:string;tone?:"warn"|"danger"}){return <div className={`card metric ${tone??""}`}><span className="metricLabel">{label}</span><strong>{value}</strong><small>{sub}</small></div>}
