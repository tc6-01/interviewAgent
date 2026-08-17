import type { InterviewFocus } from "../types/ui";

const skillPatterns: Array<[string, RegExp]> = [
  ["Go", /\bgo(lang)?\b|goroutine|gin|gmp/i],
  ["Java", /\bjava\b|spring|jvm/i],
  ["Python", /\bpython\b|django|fastapi/i],
  ["React", /\breact\b|next\.js|前端/i],
  ["Vue", /\bvue\b|nuxt/i],
  ["MySQL", /mysql|sql|数据库/i],
  ["Redis", /redis|缓存/i],
  ["消息队列", /kafka|rocketmq|rabbitmq|消息队列|mq/i],
  ["微服务", /microservice|微服务|grpc/i],
  ["分布式系统", /分布式|高并发|高可用|distributed/i],
  ["容器与云原生", /docker|kubernetes|k8s|云原生/i],
  ["AI / LLM", /llm|大模型|agent|rag|人工智能|机器学习/i],
];

function detectPosition(jd: string): string {
  const titleLine = jd
    .split(/\n+/)
    .map((line) => line.trim())
    .find((line) => /工程师|开发|架构师|经理|专家|developer|engineer/i.test(line));

  if (titleLine && titleLine.length <= 36) return titleLine.replace(/^职位[:：]?\s*/, "");
  if (/前端|react|vue/i.test(jd)) return "前端研发工程师";
  if (/go(lang)?|后端|服务端/i.test(jd)) return "后端研发工程师";
  if (/算法|机器学习|llm|大模型/i.test(jd)) return "AI / 算法工程师";
  return "软件研发工程师";
}

function detectLevel(text: string): string {
  const years = [...text.matchAll(/(\d+)\s*年/g)].map((match) => Number(match[1]));
  const maxYears = years.length > 0 ? Math.max(...years) : 0;
  if (/专家|架构师|tech lead|负责人/i.test(text) || maxYears >= 8) return "专家 / 架构方向";
  if (/高级|资深|senior/i.test(text) || maxYears >= 5) return "高级";
  if (/中级|3\s*[-~至]\s*5\s*年/i.test(text) || maxYears >= 3) return "中级";
  return "基础到中级";
}

export function deriveInterviewFocus(jd: string, resume: string): InterviewFocus {
  const jdSkills = skillPatterns.filter(([, pattern]) => pattern.test(jd)).map(([name]) => name);
  const resumeSkills = skillPatterns.filter(([, pattern]) => pattern.test(resume)).map(([name]) => name);
  const matchedSkills = jdSkills.filter((skill) => resumeSkills.includes(skill));
  const focusAreas = [...jdSkills, ...resumeSkills]
    .filter((skill, index, list) => list.indexOf(skill) === index)
    .slice(0, 6);

  return {
    position: detectPosition(jd),
    level: detectLevel(`${jd}\n${resume}`),
    focusAreas: focusAreas.length > 0 ? focusAreas : ["项目经验", "基础能力", "系统设计"],
    matchedSkills,
  };
}
