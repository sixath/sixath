const VERB_MAP: Record<string, string> = {
  read_file: '读取文件',
  write_file: '写入文件',
  execute_query: '数据库查询',
  execute_write: '数据库写入',
  web_search: '网页搜索',
  web_fetch: '抓取网页',
  skill_manage: '技能管理',
  ask_user: '询问用户',
  tool_search: '工具检索',
  session_search: '会话检索',
  append_learning: '记录经验',
}

export function toolVerb(name: string): string {
  if (!name) return '工具调用'
  return VERB_MAP[name] ?? name
}
