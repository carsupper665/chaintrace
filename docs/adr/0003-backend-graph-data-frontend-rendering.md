# Backend graph data with frontend rendering

The backend owns normalized transaction graph data, de-duplication, aggregate metrics, and paginated access to an analyzed dataset, but it does not own node coordinates, visual grouping, zoom, pan, selection, or view history. Frontend graph expansion reveals more data from the same analyzed dataset, and the frontend may enrich backend graph data with the rendering fields required by its existing contract without coupling the backend to one visualization.
