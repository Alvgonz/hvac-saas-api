-- Enable UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- =========================
-- ENUMS
-- =========================
DO $$ BEGIN
  CREATE TYPE user_role AS ENUM ('admin','dispatcher','technician','client');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE asset_type AS ENUM ('split','mini_split','package_unit','vrf','chiller','other');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE work_order_status AS ENUM ('open','assigned','in_progress','completed','cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE work_order_type AS ENUM ('preventive','corrective','inspection');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE work_order_priority AS ENUM ('low','medium','high','critical');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
  CREATE TYPE work_order_file_type AS ENUM ('photo','pdf','other');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- =========================
-- TABLES
-- =========================

CREATE TABLE IF NOT EXISTS service_provider (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name varchar(100) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS customer (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  name varchar(120) NOT NULL,

  contact_name varchar(80),
  contact_email varchar(120),
  contact_phone varchar(20),

  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS "user" (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  customer_id uuid REFERENCES customer(id),

  fullname varchar(70) NOT NULL,
  email varchar(100) NOT NULL,
  password varchar(255) NOT NULL,
  phone_number varchar(20),

  role user_role NOT NULL DEFAULT 'technician',

  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_user_provider_email
  ON "user"(service_provider_id, email);

CREATE TABLE IF NOT EXISTS site (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  customer_id uuid NOT NULL REFERENCES customer(id),

  name varchar(120) NOT NULL,
  address varchar(200),

  is_active boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS area (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  site_id uuid NOT NULL REFERENCES site(id),

  name varchar(120) NOT NULL,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS asset (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  customer_id uuid NOT NULL REFERENCES customer(id),

  site_id uuid NOT NULL REFERENCES site(id),
  area_id uuid REFERENCES area(id),

  type asset_type NOT NULL DEFAULT 'other',

  tag_code varchar(60) NOT NULL,
  name varchar(120),

  manufacturer varchar(80),
  model varchar(80),
  serial_number varchar(80),

  capacity_btu int,
  refrigerant_type varchar(20),

  install_date date,
  status varchar(20) NOT NULL DEFAULT 'active',

  notes varchar(500),

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),

  CONSTRAINT uq_asset_customer_tag UNIQUE (customer_id, tag_code)
);

CREATE TABLE IF NOT EXISTS work_order (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),

  customer_id uuid NOT NULL REFERENCES customer(id),
  site_id uuid NOT NULL REFERENCES site(id),
  asset_id uuid NOT NULL REFERENCES asset(id),

  type work_order_type NOT NULL DEFAULT 'corrective',
  priority work_order_priority NOT NULL DEFAULT 'medium',
  status work_order_status NOT NULL DEFAULT 'open',

  title varchar(140) NOT NULL,
  description varchar(2000),
  notes varchar(4000),

  created_by uuid NOT NULL REFERENCES "user"(id),
  assigned_to uuid REFERENCES "user"(id),

  scheduled_at timestamptz,
  started_at timestamptz,
  completed_at timestamptz,

  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS work_order_attachment (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  work_order_id uuid NOT NULL REFERENCES work_order(id) ON DELETE CASCADE,

  file_url varchar(500) NOT NULL,
  file_type work_order_file_type NOT NULL DEFAULT 'photo',
  uploaded_by uuid NOT NULL REFERENCES "user"(id),

  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS work_order_comment (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  service_provider_id uuid NOT NULL REFERENCES service_provider(id),
  work_order_id uuid NOT NULL REFERENCES work_order(id) ON DELETE CASCADE,

  author_id uuid NOT NULL REFERENCES "user"(id),
  comment varchar(2000) NOT NULL,

  created_at timestamptz NOT NULL DEFAULT now()
);
