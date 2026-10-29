"""scoring/learned.py：兩種學可疑行為的方法，在合成資料上要能把「像可疑樣本的」排在前面。"""

import numpy as np
import pytest

from scoring.learned import SCORERS

rng = np.random.default_rng(0)
SUSPICIOUS = rng.normal(2.0, 0.5, size=(200, 5))
UNLABELED = rng.normal(0.0, 1.0, size=(1000, 5))


@pytest.mark.parametrize("scorer", SCORERS, ids=lambda s: s.kind)
def test_ranks_suspicious_looking_rows_above_ordinary_ones(scorer):
    model = scorer().fit(SUSPICIOUS, UNLABELED)
    looks_suspicious = model.score(rng.normal(2.0, 0.5, size=(50, 5)))
    looks_ordinary = model.score(rng.normal(0.0, 1.0, size=(50, 5)))

    assert looks_suspicious.shape == (50,)
    assert np.median(looks_suspicious) > np.median(looks_ordinary)


def test_gbdt_never_takes_more_negatives_than_exist():
    model = SCORERS[0]().fit(SUSPICIOUS, UNLABELED[:10])
    assert model.score(UNLABELED[:3]).shape == (3,)
