-- Café inventory: the stock signal that triggers procurement. Counts are
-- maintained by the console (simulated POS/breakage), not by PayPal.
CREATE TABLE inventory (
    sku         text PRIMARY KEY,
    title       text        NOT NULL,
    category    text        NOT NULL,
    on_hand     integer     NOT NULL CHECK (on_hand >= 0),
    par_level   integer     NOT NULL CHECK (par_level > 0),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

INSERT INTO inventory (sku, title, category, on_hand, par_level) VALUES
    ('pitcher-20oz', '20oz Frothing Pitcher', 'frothing-pitchers', 2, 8),
    ('grinder-burr-64', '64mm Burr Grinder', 'coffee-grinders', 1, 1);

CREATE TABLE agent_runs (
    id          text PRIMARY KEY,
    mandate_id  text        NOT NULL REFERENCES mandates (id),
    sku         text        NOT NULL,
    status      text        NOT NULL,
    final       text,
    steps       jsonb       NOT NULL DEFAULT '[]',
    intent_id   text,
    error       text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
