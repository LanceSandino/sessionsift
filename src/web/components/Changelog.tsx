/** @module Changelog @description Presents bounded semantic release history from the local API. */
import React,{useEffect,useState} from "react";
import {api} from "../api";
import type {ChangelogPage} from "../types";

export function Changelog(){
  const[page,setPage]=useState(1);
  const[data,setData]=useState<ChangelogPage>();
  const[error,setError]=useState("");
  useEffect(()=>{let active=true;setError("");api.changelog(page).then(value=>{if(active)setData(value)}).catch(reason=>{if(active)setError(String(reason))});return()=>{active=false}},[page]);
  if(error)return <div className="state-message error" role="alert"><h2>Release history unavailable</h2><p>{error}</p></div>;
  if(!data)return <div className="state-message" role="status"><h2>Loading release history…</h2><p>Reading the bounded changelog from this SessionSift build.</p></div>;
  return <div className="release-page">
    <div className="release-summary"><span className="status-pill">Current version {data.current_version}</span><span>{data.total} semantic release{data.total===1?"":"s"}</span></div>
    {data.releases.map(item=><article className="release-card" key={item.version}>
      <header><div><span className="eyebrow">Version {item.version}</span><h2>{item.title}</h2></div><time dateTime={item.date}>{item.date}</time></header>
      {item.changes.map(group=><section key={group.category}><h3>{group.category}</h3><ul>{group.items.map(change=><li key={change}>{change}</li>)}</ul></section>)}
    </article>)}
    <nav className="release-pagination" aria-label="Changelog pages"><button type="button" className="button" aria-label="Previous releases" disabled={!data.has_previous} onClick={()=>setPage(value=>value-1)}>Previous</button><span>Page {data.page} of {data.total_pages}</span><button type="button" className="button" aria-label="Next releases" disabled={!data.has_next} onClick={()=>setPage(value=>value+1)}>Next</button></nav>
  </div>;
}
