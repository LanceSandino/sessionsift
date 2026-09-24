/** @module Icon @description Renders the workspace's inline stroke icons; decorative unless a label is given. */
import React from "react";

const paths={
  chat:<><path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-1 2V11.5A8.5 8.5 0 0 1 11.5 3h1a8.5 8.5 0 0 1 8.5 8.5Z"/><path d="M7 9h10M7 13h7"/></>,
  chart:<path d="M4 3v17h17M8 15v-4m5 4V6m5 9V9"/>,
  link:<path d="m10 13 4-4m-7 6-2 2a4 4 0 0 1-5-5l4-4a4 4 0 0 1 6 0m4-2 2-2a4 4 0 0 1 6 6l-4 4a4 4 0 0 1-6 0" transform="translate(1 1)"/>,
  compare:<path d="M7 4 3 8l4 4M3 8h13m1 4 4 4-4 4m4-4H8"/>,
  health:<path d="M3 12h4l2-5 4 10 2-5h6"/>,
  sun:<><circle cx="12" cy="12" r="4"/><path d="M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5"/></>,
  moon:<path d="M20 14.5A8 8 0 0 1 9.5 4 8 8 0 1 0 20 14.5Z"/>,
  plus:<path d="M12 5v14M5 12h14"/>,
  search:<><circle cx="10.5" cy="10.5" r="6.5"/><path d="m16 16 5 5"/></>,
  import:<path d="M4 15v5h16v-5M12 3v12m-4-4 4 4 4-4"/>,
  lock:<><rect x="5" y="10" width="14" height="11" rx="2"/><path d="M8 10V6a4 4 0 0 1 8 0v4m-4 5v2"/></>,
  folder:<path d="M3 6V4h6l2 3h10v13H3V6Z"/>,
  focus:<><path d="M3 8V3h5m8 0h5v5M3 16v5h5m8 0h5v-5"/><rect x="8" y="8" width="8" height="8" rx="1"/></>,
  panel:<><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/></>,
  users:<><circle cx="9" cy="8" r="3"/><path d="M3 21v-3a6 6 0 0 1 12 0v3m3-12v6m-3-3h6"/></>,
  user:<><circle cx="12" cy="8" r="4"/><path d="M4 21v-1a8 8 0 0 1 16 0v1"/></>,
  "circle-check":<><circle cx="12" cy="12" r="9"/><path d="m8 12 3 3 5-6"/></>,
  timeline:<><path d="M6 4v16m5-14h9m-9 6h6m-6 6h9"/><circle cx="6" cy="6" r="2" fill="var(--paper)"/><circle cx="6" cy="12" r="2" fill="var(--paper)"/><circle cx="6" cy="18" r="2" fill="var(--paper)"/></>,
  tool:<path d="M14 5a5 5 0 0 0-6 6l-5 5a3 3 0 0 0 4 4l5-5a5 5 0 0 0 7-6l-3 3-4-4 3-3Z"/>,
  note:<path d="M13 3H4v18h16V11M8 8h3m-3 5h2m4-5 6-6 3 3-6 6-4 1 1-4Z"/>,
  code:<path d="m7 6-5 6 5 6m10-12 5 6-5 6m-3-15-4 18"/>,
  x:<path d="m6 6 12 12M6 18 18 6"/>,
  chevron:<path d="m9 5 7 7-7 7"/>,
  arrow:<path d="M4 12h15m-5-5 5 5-5 5"/>,
  check:<path d="m5 12 4 4L19 6"/>,
  warning:<><path d="m12 3 10 18H2L12 3Z"/><path d="M12 9v5m0 3h.01"/></>,
  file:<><path d="M14 2H5v20h14V7l-5-5Z"/><path d="M14 2v6h5M8 12h8m-8 4h5"/></>,
  compact:<path d="M4 5h16M4 19h16m-8-12v4m-3-2 3 3 3-3m-3 8v-4m-3 2 3-3 3 3"/>,
  send:<path d="M12 20V4m-6 6 6-6 6 6"/>,
  menu:<path d="M4 6h16M4 12h16M4 18h16"/>,
  clock:<><circle cx="12" cy="12" r="9"/><path d="M12 7v6l4 2"/></>,
  copy:<><rect x="8" y="8" width="12" height="13" rx="2"/><path d="M16 8V3H3v13h5"/></>,
  terminal:<path d="m5 6 6 6-6 6m8 0h6"/>,
  shield:<><path d="m12 2 9 4v6c0 5-9 10-9 10S3 17 3 12V6l9-4Z"/><path d="m8 11 3 3 5-6"/></>,
  filter:<path d="M3 5h18l-7 8v6l-4 2v-8L3 5Z"/>,
  external:<path d="M14 4h6v6m0-6-9 9M19 14v6H4V5h6"/>,
  steer:<path d="M4 12h10m-4-4 4 4-4 4M20 5v14"/>,
  spark:<path d="M12 3v4m0 10v4M3 12h4m10 0h4M6 6l2.5 2.5m7 7L18 18M6 18l2.5-2.5m7-7L18 6"/>,
} as const;

export type IconName=keyof typeof paths;

export function Icon({name,label,className=""}:{name:IconName;label?:string;className?:string}){
  return <svg className={`icon ${className}`.trim()} viewBox="0 0 24 24" role={label?"img":undefined} aria-label={label} aria-hidden={label?undefined:true} focusable="false">{paths[name]}</svg>;
}
