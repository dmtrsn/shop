-- Rolling back the demo account intentionally removes orders created through
-- that test account as well, while leaving every non-demo user's data intact.
DELETE FROM orders WHERE user_id = 'usr-demo';
DELETE FROM favorites WHERE user_id = 'usr-demo';
DELETE FROM cart_items WHERE user_id = 'usr-demo';
DELETE FROM collection_products WHERE collection_id IN ('col-new','col-bestsellers','col-gifts','col-no-sugar');
DELETE FROM collections WHERE id IN ('col-new','col-bestsellers','col-gifts','col-no-sugar');
DELETE FROM products WHERE id IN ('p01','p02','p03','p04','p05','p06','p07','p08','p09','p10','p11','p12','p13','p14','p15','p16','p17','p18','p19','p20','p21','p22','p23','p24');
DELETE FROM brands WHERE id IN ('brand-unbranded','b-atelier-noir','b-atelier-maison','b-caffarel','b-piemonte','b-leonidas','b-manon','b-sweetzerland','b-belev','b-kolomna','b-michel','b-lakrids');
DELETE FROM manufacturers WHERE id IN ('m-choco','m-belyov','m-caffarel','m-leonidas','m-sweetzerland','m-kolomna','m-michel','m-lakrids');
DELETE FROM categories WHERE id IN ('cat-bars','cat-dragee','cat-praline','cat-truffles','cat-chocolate','cat-candy','cat-cookies','cat-marshmallow','cat-marmalade','cat-gifts');
DELETE FROM users WHERE id = 'usr-demo';
