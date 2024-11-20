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

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: swift_wallets; Type: TABLE; Schema: public; Owner: swift-admin
--

CREATE TABLE public.swift_wallets (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    customer_id bigint NOT NULL,
    type character varying(50) NOT NULL,
    currency character varying(3) NOT NULL,
    balance numeric(19,4) DEFAULT 0,
    status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL,
    updated_at timestamp without time zone DEFAULT now() NOT NULL,
    CONSTRAINT positive_balance CHECK ((balance >= (0)::numeric))
);


ALTER TABLE public.swift_wallets OWNER TO "swift-admin";

--
-- Name: swift_wallets_customer_id_seq; Type: SEQUENCE; Schema: public; Owner: swift-admin
--

CREATE SEQUENCE public.swift_wallets_customer_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER TABLE public.swift_wallets_customer_id_seq OWNER TO "swift-admin";

--
-- Name: swift_wallets_customer_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: swift-admin
--

ALTER SEQUENCE public.swift_wallets_customer_id_seq OWNED BY public.swift_wallets.customer_id;


--
-- Name: swift_wallets customer_id; Type: DEFAULT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.swift_wallets ALTER COLUMN customer_id SET DEFAULT nextval('public.swift_wallets_customer_id_seq'::regclass);


--
-- Data for Name: swift_wallets; Type: TABLE DATA; Schema: public; Owner: swift-admin
--

COPY public.swift_wallets (id, customer_id, type, currency, balance, status, created_at, updated_at) FROM stdin;
3a33b9a2-4700-4594-8ce6-3334bd804156	1	personal	NGN	0.0000	active	2024-11-17 14:30:00.821007	2024-11-17 14:30:00.821007
ddc457ab-8962-466b-966d-bd15aae22ad1	1	personal	NGN	0.0000	active	2024-11-17 14:30:43.338155	2024-11-17 14:30:43.338155
\.


--
-- Name: swift_wallets_customer_id_seq; Type: SEQUENCE SET; Schema: public; Owner: swift-admin
--

SELECT pg_catalog.setval('public.swift_wallets_customer_id_seq', 1, false);


--
-- Name: swift_wallets swift_wallets_pkey; Type: CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.swift_wallets
    ADD CONSTRAINT swift_wallets_pkey PRIMARY KEY (id);


--
-- Name: idx_accounts_customer; Type: INDEX; Schema: public; Owner: swift-admin
--

CREATE INDEX idx_accounts_customer ON public.swift_wallets USING btree (customer_id);


--
-- Name: swift_wallets update_accounts_updated_at; Type: TRIGGER; Schema: public; Owner: swift-admin
--

CREATE TRIGGER update_accounts_updated_at BEFORE UPDATE ON public.swift_wallets FOR EACH ROW EXECUTE FUNCTION public.update_updated_at_column();


--
-- Name: swift_wallets swift_wallets_customer_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: swift-admin
--

ALTER TABLE ONLY public.swift_wallets
    ADD CONSTRAINT swift_wallets_customer_id_fkey FOREIGN KEY (customer_id) REFERENCES public.users(id);


--
-- PostgreSQL database dump complete
--

