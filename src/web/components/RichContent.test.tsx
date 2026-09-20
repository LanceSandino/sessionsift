/** @module RichContentTests @description Adversarial and readable output regression tests. */
import React from "react";
import {afterEach,describe,expect,it} from "vitest";
import {cleanup,fireEvent,render,screen} from "@testing-library/react";
import "@testing-library/jest-dom/vitest";
import {RichContent} from "./RichContent";
afterEach(cleanup);
describe("untrusted rich content",()=>{
 it("renders Markdown structure and fenced code without interpreting HTML",()=>{
  const {container}=render(<RichContent text={'# Heading\n\n**Strong** and `code`\n\n```html\n<img src=x onerror=alert(1)>\n```\n\n<script>alert(1)</script>'}/>);
  expect(screen.getByRole("heading",{name:"Heading"})).toBeInTheDocument();expect(container.querySelector("strong")).toHaveTextContent("Strong");expect(container.querySelector("script,img,iframe,svg")).toBeNull();expect(screen.getByText("<img src=x onerror=alert(1)>")).toBeInTheDocument();
 });
 it("blocks tracking images and non-HTTPS link schemes",()=>{
  const {container}=render(<RichContent text={'![tracker](https://evil.example/pixel)\n\n[bad](javascript:alert%281%29) [data](data:text/html,x) [file](file:///etc/passwd) [relative](/api/delete) [ok](https://example.com)'}/>);
  expect(container.querySelector("img")).toBeNull();expect(container.querySelectorAll("a")).toHaveLength(1);expect(screen.getByRole("link",{name:"ok"})).toHaveAttribute("rel","noopener noreferrer nofollow");
 });
 it("formats JSON and permits plain-text inspection without executing keys",()=>{
  const {container}=render(<RichContent text={'{"__proto__":{"polluted":true},"html":"<script>alert(1)</script>","count":2}'}/>);
  expect(container.querySelector("pre")).toHaveTextContent('"count": 2');expect(container.querySelector("script")).toBeNull();
  fireEvent.click(screen.getByRole("button",{name:"Show source"}));expect(container.querySelector("pre")).toHaveTextContent('"count":2');
 });
 it("bounds huge content and offers an explicit expansion",()=>{
  const {container}=render(<RichContent text={"a".repeat(110000)}/>);
  expect(container.textContent!.length).toBeLessThan(27000);fireEvent.click(screen.getByRole("button",{name:/Show more/}));expect(container.textContent!.length).toBeLessThan(102000);expect(screen.getByText(/Display capped/)).toBeInTheDocument();
 });
});
