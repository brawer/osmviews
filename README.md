# OSMViews

World-wide ranking of geographic locations based on OpenStreetMap tile logs.
Updated weekly. Aggregated over the past 52 weeks to smoothen seasonal effects.
For any location on the planet, up to ~150m/z18 resolution.


## Code repository

* `cmd/webserver` is the [OSMViews webserver](https://osmviews.toolforge.org).
* `cmd/osmviews-builder` is the pipeline that computes the data.

Client libraries are maintained in separate repositories.
For Python, see [brawer/osmviews-py](https://github.com/brawer/osmviews-py).


## Roadmap to 1.0

* Write documentation for the Python client.

* Write documentation for the backend pipeline. Document the tricks
  we use to process such a large dataset on a single machine in reasonable
  time.

* Improve the server homepage, display the histogram whose data already
  gets computed.

* Implement the OpenGIS WMTS protocol in the webserver.

* Extend the webserver homepage to display a heatmap. Currently, users
  can already point QGIS or another GIS to our GeoTIFF file, but not many
  people know how to do this.
