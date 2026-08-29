# The limits in force

`envelope.yaml` says what each caller may do, and it is the operator's file. Nothing the session runs carries it: the session asks `read_envelope` and is told only what applies to it, so no ceiling appears in a prompt, a task or a skill anywhere in this repository.

Lowering a ceiling is an edit here. The service that serves it reads the file on every question, so a session already at work sees the new number without anything being restarted.

What does NOT belong here is the point of the file. How a trade is chosen - which leg to sell, what a structure must pay, when to close - is strategy: nobody grants it, and getting around it would gain the agent nothing. It lives in the playbook skill. Keeping the two apart is what stops this file from quietly becoming the strategy.
