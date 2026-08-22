# Analysis independent of graph rendering

The backend builds and analyzes a bounded Analysis Dataset before the frontend chooses how much of its graph to render. Pagination and visual node expansion reveal portions of that same dataset and never trigger re-analysis; only a change to the Investigation's Analysis Scope produces a new dataset and result, preventing UI performance choices from changing analytical meaning.
