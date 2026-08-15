import { describe, expect, it } from "vitest";
import { deriveInterviewFocus } from "./focus";

describe("deriveInterviewFocus", () => {
  it("extracts a useful interview preview from JD and resume text", () => {
    const focus = deriveInterviewFocus(
      "高级 Go 后端工程师\n5年以上经验，熟悉 Go、MySQL、Redis 和微服务",
      "6年 Go 开发经验，负责 Redis 缓存与微服务治理",
    );

    expect(focus.position).toBe("高级 Go 后端工程师");
    expect(focus.level).toBe("高级");
    expect(focus.matchedSkills).toEqual(expect.arrayContaining(["Go", "Redis", "微服务"]));
  });
});
