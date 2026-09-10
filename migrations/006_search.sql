-- 006：全文搜索（2026-09-09，优化批 O4）
-- ngram 分词器：把文本切成 2 字符滑窗——中文没有空格分词，默认 parser 对中文完全失效，
-- ngram 是 MySQL 官方的中文全文方案。
ALTER TABLE posts ADD FULLTEXT INDEX ft_content (content) WITH PARSER ngram;
