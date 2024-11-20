--
-- PostgreSQL database dump
--

-- Dumped from database version 15.7
-- Dumped by pg_dump version 15.7

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: update_kyc_tier(); Type: FUNCTION; Schema: public; Owner: swift-admin
--

CREATE FUNCTION public.update_kyc_tier() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
DECLARE
    can_be_tier_1 BOOLEAN;
    can_be_tier_2 BOOLEAN;
    can_be_tier_3 BOOLEAN;
BEGIN
    -- Check Tier 1 requirements
    can_be_tier_1 := (
        NEW.full_name IS NOT NULL AND
        NEW.phone_number IS NOT NULL AND
        NEW.email IS NOT NULL AND
        (NEW.bvn IS NOT NULL OR NEW.nin IS NOT NULL) AND
        NEW.gender IS NOT NULL AND
        NEW.selfie_url IS NOT NULL
    );

    -- Check Tier 2 requirements (must meet Tier 1 first)
    can_be_tier_2 := (
        can_be_tier_1 AND
        NEW.bvn IS NOT NULL AND 
        NEW.nin IS NOT NULL
    );

    -- Check Tier 3 requirements (must meet Tier 2 first)
    can_be_tier_3 := (
        can_be_tier_2 AND
        NEW.proof_of_address_type IS NOT NULL AND
        NEW.proof_of_address_url IS NOT NULL AND
        NEW.proof_of_address_date IS NOT NULL AND
        -- Check if proof of address is not older than 3 months
        NEW.proof_of_address_date >= (CURRENT_DATE - INTERVAL '6 months')
    );

    -- Update tier and corresponding limits
    IF can_be_tier_3 THEN
        NEW.tier := 3;
        NEW.daily_transfer_limit_ngn := 5000000.00;
        NEW.wallet_balance_limit_ngn := NULL; -- Unlimited
    ELSIF can_be_tier_2 THEN
        NEW.tier := 2;
        NEW.daily_transfer_limit_ngn := 200000.00;
        NEW.wallet_balance_limit_ngn := 500000.00;
    ELSIF can_be_tier_1 THEN
        NEW.tier := 1;
        NEW.daily_transfer_limit_ngn := 50000.00;
        NEW.wallet_balance_limit_ngn := 200000.00;
    ELSE
        NEW.tier := 0;
        NEW.daily_transfer_limit_ngn := 0.00;
        NEW.wallet_balance_limit_ngn := 0.00;
    END IF;

    -- Update verification date if tier changed
    IF NEW.tier != OLD.tier OR OLD.tier IS NULL THEN
        NEW.verification_date := CURRENT_TIMESTAMP;
        
        -- Update status to 'active' if tier is greater than 0
        IF NEW.tier > 0 THEN
            NEW.status := 'active';
        END IF;
    END IF;

    RETURN NEW;
END;
$$;


ALTER FUNCTION public.update_kyc_tier() OWNER TO "swift-admin";

--
-- Name: update_updated_at_column(); Type: FUNCTION; Schema: public; Owner: swift-admin
--

CREATE FUNCTION public.update_updated_at_column() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;


ALTER FUNCTION public.update_updated_at_column() OWNER TO "swift-admin";

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: kyc; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.kyc (
    id bigint NOT NULL,
    user_id integer NOT NULL,
    tier integer DEFAULT 0 NOT NULL,
    daily_transfer_limit_ngn numeric(15,2) DEFAULT 0,
    wallet_balance_limit_ngn numeric(15,2) DEFAULT 0,
    status character varying(20) DEFAULT 'pending'::character varying NOT NULL,
    verification_date timestamp with time zone,
    full_name character varying(255),
    phone_number character varying(20),
    email character varying(255),
    bvn character varying(11),
    nin character varying(11),
    gender character varying(10),
    selfie_url text,
    id_type character varying(20),
    id_number character varying(50),
    id_image_url text,
    state character varying(100),
    lga character varying(100),
    house_number character varying(50),
    street_name character varying(255),
    nearest_landmark character varying(255),
    proof_of_address_type character varying(20),
    proof_of_address_url text,
    proof_of_address_date date,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    additional_info jsonb DEFAULT '{"data": {}}'::jsonb,
    CONSTRAINT kyc_daily_transfer_limit_ngn_check CHECK ((daily_transfer_limit_ngn >= (0)::numeric)),
    CONSTRAINT kyc_gender_check CHECK (((gender)::text = ANY (ARRAY[('male'::character varying)::text, ('female'::character varying)::text, ('other'::character varying)::text]))),
    CONSTRAINT kyc_id_type_check CHECK (((id_type)::text = ANY (ARRAY[('international_passport'::character varying)::text, ('voters_card'::character varying)::text, ('drivers_license'::character varying)::text]))),
    CONSTRAINT kyc_proof_of_address_type_check CHECK (((proof_of_address_type)::text = ANY (ARRAY[('utility_bill'::character varying)::text, ('bank_statement'::character varying)::text, ('tenancy_agreement'::character varying)::text]))),
    CONSTRAINT kyc_status_check CHECK (((status)::text = ANY (ARRAY[('pending'::character varying)::text, ('active'::character varying)::text, ('rejected'::character varying)::text]))),
    CONSTRAINT kyc_tier_check CHECK (((tier >= 0) AND (tier <= 3))),
    CONSTRAINT kyc_wallet_balance_limit_ngn_check CHECK ((wallet_balance_limit_ngn >= (0)::numeric))
);


ALTER TABLE public.kyc OWNER TO "swift-admin";

--
-- Name: kyc_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.kyc_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.kyc_id_seq OWNER TO "swift-admin";

--
-- Name: kyc_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.kyc_id_seq OWNED BY public.kyc.id;


--
-- Name: otps; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.otps (
    id bigint NOT NULL,
    user_id integer NOT NULL,
    otp character varying(256) NOT NULL,
    expired boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.otps OWNER TO "swift-admin";

--
-- Name: otps_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.otps_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.otps_id_seq OWNER TO "swift-admin";

--
-- Name: otps_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.otps_id_seq OWNED BY public.otps.id;


--
-- Name: proof_of_address_images; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.proof_of_address_images (
    id integer NOT NULL,
    user_id integer NOT NULL,
    filename character varying(255) NOT NULL,
    proof_type character varying(100) NOT NULL,
    image_data bytea NOT NULL,
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    verified boolean DEFAULT false NOT NULL,
    verified_at timestamp with time zone,
    CONSTRAINT verified_at_must_exist_when_verified CHECK (((verified = false) OR ((verified = true) AND (verified_at IS NOT NULL))))
);


ALTER TABLE public.proof_of_address_images OWNER TO "swift-admin";

--
-- Name: proof_of_address_images_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.proof_of_address_images_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.proof_of_address_images_id_seq OWNER TO "swift-admin";

--
-- Name: proof_of_address_images_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.proof_of_address_images_id_seq OWNED BY public.proof_of_address_images.id;


--
-- Name: referral_entries; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.referral_entries (
    id bigint NOT NULL,
    referral_key character varying(256) NOT NULL,
    referrer integer NOT NULL,
    referee integer NOT NULL,
    referral_detail character varying(256) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


ALTER TABLE public.referral_entries OWNER TO "swift-admin";

--
-- Name: referral_entries_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.referral_entries_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.referral_entries_id_seq OWNER TO "swift-admin";

--
-- Name: referral_entries_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.referral_entries_id_seq OWNED BY public.referral_entries.id;


--
-- Name: referrals; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.referrals (
    id bigint NOT NULL,
    user_id integer NOT NULL,
    referral_key character varying(256) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.referrals OWNER TO "swift-admin";

--
-- Name: referrals_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.referrals_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.referrals_id_seq OWNER TO "swift-admin";

--
-- Name: referrals_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.referrals_id_seq OWNED BY public.referrals.id;


--
-- Name: schema_migrations; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.schema_migrations (
    version bigint NOT NULL,
    dirty boolean NOT NULL
);


ALTER TABLE public.schema_migrations OWNER TO "swift-admin";

--
-- Name: users; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.users (
    id bigint NOT NULL,
    first_name character varying(50),
    last_name character varying(50),
    email character varying(256) NOT NULL,
    hashed_password character varying(256),
    hashed_passcode character varying(256),
    hashed_pin character varying(256),
    phone_number character varying(50) NOT NULL,
    role character varying(10) NOT NULL,
    verified boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at timestamp with time zone
);


ALTER TABLE public.users OWNER TO "swift-admin";

--
-- Name: users_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.users_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.users_id_seq OWNER TO "swift-admin";

--
-- Name: users_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.users_id_seq OWNED BY public.users.id;


--
-- Name: kyc id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.kyc ALTER COLUMN id SET DEFAULT nextval('public.kyc_id_seq'::regclass);


--
-- Name: otps id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.otps ALTER COLUMN id SET DEFAULT nextval('public.otps_id_seq'::regclass);


--
-- Name: proof_of_address_images id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.proof_of_address_images ALTER COLUMN id SET DEFAULT nextval('public.proof_of_address_images_id_seq'::regclass);


--
-- Name: referral_entries id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referral_entries ALTER COLUMN id SET DEFAULT nextval('public.referral_entries_id_seq'::regclass);


--
-- Name: referrals id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referrals ALTER COLUMN id SET DEFAULT nextval('public.referrals_id_seq'::regclass);


--
-- Name: users id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.users ALTER COLUMN id SET DEFAULT nextval('public.users_id_seq'::regclass);


--
-- Data for Name: kyc; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.kyc (id, user_id, tier, daily_transfer_limit_ngn, wallet_balance_limit_ngn, status, verification_date, full_name, phone_number, email, bvn, nin, gender, selfie_url, id_type, id_number, id_image_url, state, lga, house_number, street_name, nearest_landmark, proof_of_address_type, proof_of_address_url, proof_of_address_date, created_at, updated_at, additional_info) FROM stdin;
\.


--
-- Data for Name: otps; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.otps (id, user_id, otp, expired, created_at, expires_at, updated_at) FROM stdin;
1	1	3515	f	2024-11-14 10:42:09.297524+00	2024-11-14 11:12:09.294788+00	2024-11-14 10:42:09.297524+00
\.


--
-- Data for Name: proof_of_address_images; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.proof_of_address_images (id, user_id, filename, proof_type, image_data, created_at, updated_at, verified, verified_at) FROM stdin;
\.


--
-- Data for Name: referral_entries; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.referral_entries (id, referral_key, referrer, referee, referral_detail, created_at, updated_at, deleted_at) FROM stdin;
\.


--
-- Data for Name: referrals; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.referrals (id, user_id, referral_key, created_at, updated_at) FROM stdin;
\.


--
-- Data for Name: schema_migrations; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.schema_migrations (version, dirty) FROM stdin;
6	f
\.


--
-- Data for Name: users; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.users (id, first_name, last_name, email, hashed_password, hashed_passcode, hashed_pin, phone_number, role, verified, created_at, updated_at, deleted_at) FROM stdin;
1	Johnpaul	Muoneme	thurggex+swiftfiat@gmail.com	$2a$10$HmGbxJEfDa8u0iPt0mUEyO5qi4iKjsG5bsMjm37JeELVADkNtKIey	\N	\N	+2348013332242	user	t	2024-11-14 10:41:35.505584+00	2024-11-14 10:42:34.509988+00	\N
\.


--
-- Name: kyc_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.kyc_id_seq', 1, false);


--
-- Name: otps_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.otps_id_seq', 1, true);


--
-- Name: proof_of_address_images_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.proof_of_address_images_id_seq', 1, false);


--
-- Name: referral_entries_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.referral_entries_id_seq', 1, false);


--
-- Name: referrals_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.referrals_id_seq', 1, false);


--
-- Name: users_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.users_id_seq', 3, true);


--
-- Name: kyc kyc_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.kyc
    ADD CONSTRAINT kyc_pkey PRIMARY KEY (id);


--
-- Name: kyc kyc_user_id_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.kyc
    ADD CONSTRAINT kyc_user_id_key UNIQUE (user_id);


--
-- Name: otps otps_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.otps
    ADD CONSTRAINT otps_pkey PRIMARY KEY (id);


--
-- Name: otps otps_user_id_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.otps
    ADD CONSTRAINT otps_user_id_key UNIQUE (user_id);


--
-- Name: proof_of_address_images proof_of_address_images_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.proof_of_address_images
    ADD CONSTRAINT proof_of_address_images_pkey PRIMARY KEY (id);


--
-- Name: referral_entries referral_entries_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referral_entries
    ADD CONSTRAINT referral_entries_pkey PRIMARY KEY (id);


--
-- Name: referral_entries referral_entries_referee_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referral_entries
    ADD CONSTRAINT referral_entries_referee_key UNIQUE (referee);


--
-- Name: referrals referrals_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referrals
    ADD CONSTRAINT referrals_pkey PRIMARY KEY (id);


--
-- Name: referrals referrals_user_id_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referrals
    ADD CONSTRAINT referrals_user_id_key UNIQUE (user_id);


--
-- Name: schema_migrations schema_migrations_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.schema_migrations
    ADD CONSTRAINT schema_migrations_pkey PRIMARY KEY (version);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_phone_number_key; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_phone_number_key UNIQUE (phone_number);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: idx_kyc_user_id; Type: INDEX; Schema: public; Owner: swift-admin
--

CREATE INDEX idx_kyc_user_id ON public.kyc USING btree (user_id);


--
-- Name: kyc trigger_update_kyc_tier; Type: TRIGGER; Schema: public; Owner: swift-admin
--

CREATE TRIGGER trigger_update_kyc_tier BEFORE INSERT OR UPDATE ON public.kyc FOR EACH ROW EXECUTE FUNCTION public.update_kyc_tier();


--
-- Name: kyc update_kyc_updated_at; Type: TRIGGER; Schema: public; Owner: swift-admin
--

CREATE TRIGGER update_kyc_updated_at BEFORE UPDATE ON public.kyc FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();


--
-- Name: kyc kyc_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.kyc
    ADD CONSTRAINT kyc_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: otps otps_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.otps
    ADD CONSTRAINT otps_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: proof_of_address_images proof_of_address_images_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.proof_of_address_images
    ADD CONSTRAINT proof_of_address_images_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: referral_entries referral_entries_referee_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referral_entries
    ADD CONSTRAINT referral_entries_referee_fkey FOREIGN KEY (referee) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: referral_entries referral_entries_referrer_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referral_entries
    ADD CONSTRAINT referral_entries_referrer_fkey FOREIGN KEY (referrer) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: referrals referrals_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.referrals
    ADD CONSTRAINT referrals_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

