"""學習式風險評分。

這個套件跟 LLM 那一側完全無關：不 import llm / prompt / session / turn，
也不被它們 import。整個資料夾刪掉，agent 照樣啟動（見 docs/adr/0015）。

train/ 是離線用的，伺服器不會 import 它，所以這裡不轉出。
"""

from .features import FEATURE_NAMES, Transfer, compute, vector

__all__ = ["FEATURE_NAMES", "Transfer", "compute", "vector"]
