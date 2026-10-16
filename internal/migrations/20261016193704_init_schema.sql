-- +goose Up
CREATE TABLE problems (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL,
    depot_lat   double precision NOT NULL,
    depot_lon   double precision NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE stops (
    id          bigserial PRIMARY KEY,
    problem_id  uuid NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    external_id text NOT NULL,              -- client's own id, e.g. "order-1042"
    lat         double precision NOT NULL,
    lon         double precision NOT NULL,
    demand      integer NOT NULL CHECK (demand > 0),
    UNIQUE (problem_id, external_id)
);

CREATE TABLE vehicles (
    id          bigserial PRIMARY KEY,
    problem_id  uuid NOT NULL REFERENCES problems(id) ON DELETE CASCADE,
    external_id text NOT NULL,
    capacity    integer NOT NULL CHECK (capacity > 0),
    UNIQUE (problem_id, external_id)
);

CREATE INDEX problems_created_idx ON problems (created_at DESC, id DESC);

-- +goose Down
DROP TABLE vehicles;
DROP TABLE stops;
DROP TABLE problems;