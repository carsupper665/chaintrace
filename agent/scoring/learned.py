"""兩種學「可疑行為」的方法。訓練（scoring/train/train.py）會兩個都訓練、用時間外推
比較，留下比較好的那個；線上（scoring/service.py）只呼叫 score()，不管是哪一種。

類比推薦系統：被凍結的地址是使用者「喜歡的影片」，模型學的是這份喜好（見 docs/adr/0018）。

  GmmScorer   只看可疑地址，學它們的行為分佈（content-based）。分數是新地址落在
              這個分佈裡的 log 機率密度。
  GbdtScorer  可疑地址加上隨機抽的未標記地址當弱負樣本（隱式回饋的 negative
              sampling）。未標記不代表正常，只代表「沒被標記過」。分數是排序用的
              0–1，不是真實的被凍結機率。

兩者都放在這裡而不是 train/：joblib 存的是類別的 import 路徑，線上載入模型時
不該 import 訓練那一側。
"""

import numpy as np
from sklearn.ensemble import HistGradientBoostingClassifier
from sklearn.mixture import GaussianMixture
from sklearn.preprocessing import QuantileTransformer

RANDOM_STATE = 20260902
# GMM 的群數由 BIC 在可疑樣本上挑，不看任何一般地址。
GMM_COMPONENTS = (1, 2, 4, 6, 8)
# 每個可疑樣本配幾個未標記的弱負樣本。
NEGATIVES_PER_POSITIVE = 5


class GmmScorer:
    kind = "gmm"

    def fit(self, suspicious, unlabeled):
        # 分位數轉換只是把每個特徵換成排名，不用到標籤；用全部資料估比較穩。
        self.scaler = QuantileTransformer(
            output_distribution="normal", n_quantiles=1000, random_state=RANDOM_STATE
        ).fit(np.vstack([suspicious, unlabeled]))
        liked = self.scaler.transform(suspicious)
        self.model = min(
            (
                GaussianMixture(k, covariance_type="full", reg_covar=1e-3, random_state=RANDOM_STATE).fit(liked)
                for k in GMM_COMPONENTS
            ),
            key=lambda model: model.bic(liked),
        )
        return self

    def score(self, rows):
        return self.model.score_samples(self.scaler.transform(np.asarray(rows, dtype=float)))


class GbdtScorer:
    kind = "gbdt"

    def fit(self, suspicious, unlabeled):
        rng = np.random.default_rng(RANDOM_STATE)
        size = min(len(unlabeled), NEGATIVES_PER_POSITIVE * len(suspicious))
        negatives = np.asarray(unlabeled)[rng.choice(len(unlabeled), size=size, replace=False)]
        self.model = HistGradientBoostingClassifier(
            max_iter=200, learning_rate=0.05, random_state=RANDOM_STATE
        ).fit(np.vstack([suspicious, negatives]), np.r_[np.ones(len(suspicious)), np.zeros(len(negatives))])
        return self

    def score(self, rows):
        return self.model.predict_proba(np.asarray(rows, dtype=float))[:, 1]


SCORERS = (GbdtScorer, GmmScorer)
