/** @module Format @description Formats local metrics and timestamps without changing their meaning. */
export function compact(value:number){return Intl.NumberFormat("en",{notation:"compact",maximumFractionDigits:1}).format(value)}
export function percent(value:number){return `${Math.round(value*100)}%`}
export function duration(ms:number){if(!ms||ms<0)return "—";const mins=Math.round(ms/60000);return mins<60?`${mins}m`:`${Math.floor(mins/60)}h ${mins%60}m`}
export function elapsed(start:string,current:string){const ms=new Date(current).getTime()-new Date(start).getTime();const mins=Math.max(0,Math.round(ms/60000));return `${String(Math.floor(mins/60)).padStart(2,"0")}:${String(mins%60).padStart(2,"0")}`}
export function absoluteTime(value:string){const date=new Date(value);if(Number.isNaN(date.getTime()))return "Unknown time";return new Intl.DateTimeFormat(undefined,{year:"numeric",month:"short",day:"numeric",hour:"numeric",minute:"2-digit",second:"2-digit",timeZoneName:"short"}).format(date)}
export function clockTime(value:string){const date=new Date(value);if(Number.isNaN(date.getTime()))return "Unknown time";return new Intl.DateTimeFormat(undefined,{hour:"2-digit",minute:"2-digit"}).format(date)}
export function shortDateTime(value:string){const date=new Date(value);if(Number.isNaN(date.getTime()))return "Unknown time";return new Intl.DateTimeFormat(undefined,{day:"numeric",month:"short",year:"numeric",hour:"2-digit",minute:"2-digit"}).format(date)}
export function shortDate(value:string){const date=new Date(value);if(Number.isNaN(date.getTime()))return "Unknown date";return new Intl.DateTimeFormat(undefined,{day:"numeric",month:"short",year:"numeric"}).format(date)}
export function plural(count:number,noun:string){return `${count} ${noun}${count===1?"":"s"}`}
export function span(start:string,end:string){return duration(Math.max(0,new Date(end).getTime()-new Date(start).getTime()))}
export function twoDigits(value:number){return String(value).padStart(2,"0")}

/** A one-line plain-text preview: Markdown markers and whitespace runs collapse. */
export function excerpt(text:string|undefined,max:number){
  // Only Markdown syntax goes: identifiers keep their underscores and "#482" keeps its hash.
  // A heading, or a line that is wholly bold, keeps a colon so it does not run into the sentence after it.
  const heading=(_:string,title:string)=>/[.:!?]$/.test(title)?title:`${title}:`;
  const plain=(text||"").replace(/```[\s\S]*?```/g," ").replace(/^\s*#{1,6}\s+(.+?)[ \t#]*$/gm,heading).replace(/^[ \t]*\*\*([^*\n]+?)\*\*[ \t]*$/gm,heading).replace(/^\s*(?:>|[-*+]|\d+[.)])\s+/gm,"").replace(/[`*[\]|]/g,"").replace(/\s+/g," ").trim();
  return plain.length>max?`${plain.slice(0,Math.max(0,max-1)).trimEnd()}…`:plain;
}
