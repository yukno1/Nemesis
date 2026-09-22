-- 预置本地模型（Ollama）：让 make run 开箱即用
-- 决定走哪家供应商的是本表的 provider_id（llm.local 目前未被代码消费）

INSERT INTO model_configs
  (provider_id, model_name, alias, type, context_window, max_output,
   input_price, output_price, priority, capabilities, is_default, status)
SELECT p.id, 'qwen3:4b', 'qwen3:4b', 'chat', 32768, 4096, 0, 0, 10,
       '["function_call","reasoning"]'::jsonb, TRUE, 1
FROM model_providers p
WHERE p.code = 'ollama'
  AND NOT EXISTS (SELECT 1 FROM model_configs mc
                  WHERE mc.model_name = 'qwen3:4b' AND mc.type = 'chat');

INSERT INTO model_configs
  (provider_id, model_name, alias, type, context_window, max_output,
   input_price, output_price, priority, capabilities, is_default, status)
SELECT p.id, 'qwen3-embedding:0.6b', 'qwen3-embedding:0.6b', 'embedding', 8192, 0, 0, 0, 10,
       '[]'::jsonb, TRUE, 1
FROM model_providers p
WHERE p.code = 'ollama'
  AND NOT EXISTS (SELECT 1 FROM model_configs mc
                  WHERE mc.model_name = 'qwen3-embedding:0.6b' AND mc.type = 'embedding');
