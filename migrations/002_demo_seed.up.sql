INSERT INTO users (id, name, email, phone, password_hash, created_at) VALUES
('usr-demo', 'Алексей Морозов', 'demo@sugar.ru', '+7 999 123-45-67', '$2y$10$QTEU/WwQc6q9ngaHUfFn7uon8VZnVmK5Q6osIfgo.qFoxqLjyWTAm', '2025-11-04T10:00:00Z');

INSERT INTO categories (id, slug, name, description, parent_id, icon, sort_order) VALUES
('cat-chocolate', 'chocolate', 'Шоколад', 'Плитки, драже и шоколадные наборы', NULL, 'chocolate', 10),
('cat-bars', 'chocolate-bars', 'Плиточный шоколад', '', 'cat-chocolate', '', 11),
('cat-dragee', 'dragee', 'Драже в шоколаде', '', 'cat-chocolate', '', 12),
('cat-candy', 'candy', 'Конфеты', 'Пралине, трюфели, карамель', NULL, 'candy', 20),
('cat-praline', 'praline', 'Пралине', '', 'cat-candy', '', 21),
('cat-truffles', 'truffles', 'Трюфели', '', 'cat-candy', '', 22),
('cat-cookies', 'cookies-wafers', 'Печенье и вафли', '', NULL, 'cookie', 30),
('cat-marshmallow', 'marshmallow-pastila', 'Зефир и пастила', '', NULL, 'cloud', 40),
('cat-marmalade', 'marmalade', 'Мармелад', '', NULL, 'gem', 50),
('cat-gifts', 'gift-boxes', 'Подарочные наборы', '', NULL, 'gift', 60);

INSERT INTO manufacturers (id, slug, name, country, description, sort_order) VALUES
('m-choco', 'chocoatelier', 'Choco Atelier', 'Россия', 'Bean-to-bar шоколад из Петербурга', 10),
('m-belyov', 'belevskie-sladosti', 'Белёвские сладости', 'Россия', 'Пастила по традиционным рецептам', 20),
('m-caffarel', 'caffarel', 'Caffarel', 'Италия', 'Пьемонтский шоколад и джандуйя', 30),
('m-leonidas', 'leonidas', 'Leonidas', 'Бельгия', 'Бельгийские пралине с 1913 года', 40),
('m-sweetzerland', 'sweetzerland', 'Sweetzerland', 'Швейцария', 'Современная швейцарская шоколадная школа', 50),
('m-kolomna', 'kolomna', 'Коломна', 'Россия', 'Фруктовая пастила и мармелад', 60),
('m-michel', 'michel-augustin', 'Michel et Augustin', 'Франция', 'Французское сливочное печенье', 70),
('m-lakrids', 'lakrids-by-bulow', 'Lakrids by Bülow', 'Дания', 'Лакричное драже ручной работы', 80);

INSERT INTO brands (id,slug,name,manufacturer_id,description,sort_order) VALUES
('brand-unbranded','unbranded','Без бренда','m-choco','',999),
('b-atelier-noir','atelier-noir','Atelier Noir','m-choco','Тёмный шоколад и драже',10),
('b-atelier-maison','atelier-maison','Atelier Maison','m-choco','Конфеты и подарочные коллекции',20),
('b-caffarel','caffarel','Caffarel','m-caffarel','',30),
('b-piemonte','piemonte-1826','Piemonte 1826','m-caffarel','',40),
('b-leonidas','leonidas','Leonidas','m-leonidas','',50),
('b-manon','manon','Manon','m-leonidas','',60),
('b-sweetzerland','sweetzerland','Sweetzerland','m-sweetzerland','',70),
('b-belev','belev','Белёвская классика','m-belyov','',80),
('b-kolomna','kolomna-posad','Коломенский посад','m-kolomna','',90),
('b-michel','michel-augustin','Michel et Augustin','m-michel','',100),
('b-lakrids','lakrids','Lakrids','m-lakrids','',110);

INSERT INTO products (id, slug, sku, name, short_name, description, price_minor, old_price_minor, unit, weight_grams, image, gallery, category_id, manufacturer_id, tags, badges, ingredients, nutrition, shelf_life_days, rating, reviews_count, stock, featured, created_at) VALUES
('p01','dark-chocolate-sea-salt','CHA-701','Тёмный шоколад 70% с морской солью','Тёмный 70% · соль','Выразительный вкус и аккуратно подобранные ингредиенты.',48900,54900,'плитка 90 г',90,'/assets/product-chocolate.webp',ARRAY['/assets/product-chocolate.webp'],'cat-bars','m-choco',ARRAY['без сахара','веган'],ARRAY['Хит'],'Какао, какао-масло, морская соль','',180,4.9,142,24,true,'2026-01-10'),
('p02','milk-chocolate-hazelnut','CAF-122','Молочный шоколад с цельным фундуком','Молочный · фундук','Пьемонтский фундук и сливочный шоколад.',62900,0,'плитка 100 г',100,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-bars','m-caffarel',ARRAY['орехи'],ARRAY['Новинка'],'','',180,4.8,92,18,true,'2026-07-20'),
('p03','raspberry-ruby-chocolate','SWE-204','Розовый шоколад с малиной','Ruby · малина','Яркий ягодный шоколад.',71900,0,'плитка 85 г',85,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-bars','m-sweetzerland',ARRAY['ягоды'],ARRAY['Новинка'],'','',180,4.7,53,9,true,'2026-08-01'),
('p04','gianduja-praline-box','CAF-310','Джандуйя Classico','Джандуйя','Пралине с насыщенным ореховым вкусом.',159000,179000,'коробка 165 г',165,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-praline','m-caffarel',ARRAY['орехи','подарок'],ARRAY['−11%'],'','',150,4.9,164,15,true,'2026-02-15'),
('p05','leonidas-assorted-pralines','LEO-505','Ассорти бельгийских пралине','Пралине ассорти','Классическое бельгийское ассорти.',239000,0,'коробка 250 г',250,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-praline','m-leonidas',ARRAY['подарок','орехи'],ARRAY['Выбор покупателей'],'','',120,5.0,218,7,true,'2026-03-03'),
('p06','salted-caramel-truffles','CHA-422','Трюфели с солёной карамелью','Трюфели · карамель','Мягкий ганаш и солёная карамель.',129000,0,'коробка 180 г',180,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-truffles','m-choco',ARRAY['карамель','подарок'],ARRAY[]::text[],'','',90,4.8,116,13,true,'2026-03-14'),
('p07','espresso-truffles','SWE-418','Трюфели Espresso','Трюфели · кофе','Трюфели с кофейным ганашем.',139000,0,'коробка 12 шт.',160,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-truffles','m-sweetzerland',ARRAY['кофе','подарок'],ARRAY['Осталось мало'],'','',90,4.6,67,4,false,'2026-04-01'),
('p08','cocoa-almond-dragee','CHA-615','Миндаль в какао','Миндаль · какао','Обжаренный миндаль в тонком слое шоколада.',84900,0,'банка 140 г',140,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-dragee','m-choco',ARRAY['орехи','без глютена'],ARRAY[]::text[],'','',180,4.7,74,31,false,'2026-04-20'),
('p09','lakrids-gold','LAK-011','Лакричное драже Gold','Lakrids Gold','Датское лакричное драже.',249000,279000,'банка 295 г',295,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-dragee','m-lakrids',ARRAY['лакрица','подарок'],ARRAY['Лимитированная серия'],'','',180,4.5,39,6,false,'2026-05-01'),
('p10','apple-pastila-classic','BEL-101','Белёвская пастила яблочная','Пастила · яблоко','Пастила из печёных яблок.',39900,0,'коробка 200 г',200,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marshmallow','m-belyov',ARRAY['без сахара','постное'],ARRAY['Хит'],'','',120,4.9,341,42,true,'2026-05-13'),
('p11','pear-pastila','BEL-118','Пастила с грушей','Пастила · груша','Воздушная фруктовая пастила.',42900,0,'коробка 180 г',180,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marshmallow','m-belyov',ARRAY['без сахара','постное'],ARRAY[]::text[],'','',120,4.7,88,28,false,'2026-05-21'),
('p12','vanilla-marshmallow','BEL-205','Зефир ванильный','Зефир · ваниль','Классический ванильный зефир.',37900,41900,'коробка 250 г',250,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marshmallow','m-belyov',ARRAY['ваниль'],ARRAY['−10%'],'','',90,4.6,102,34,false,'2026-06-01'),
('p13','blackcurrant-marshmallow','KOL-214','Зефир из чёрной смородины','Зефир · смородина','Ягодный зефир с лёгкой кислинкой.',46900,0,'коробка 220 г',220,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marshmallow','m-kolomna',ARRAY['ягоды'],ARRAY['Новинка'],'','',90,4.8,72,17,false,'2026-07-28'),
('p14','pear-marmalade','KOL-330','Мармелад из груши с ванилью','Мармелад · груша','Фруктовый мармелад с ванилью.',51900,0,'коробка 160 г',160,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marmalade','m-kolomna',ARRAY['без глютена','фрукты'],ARRAY[]::text[],'','',150,4.7,82,21,true,'2026-06-15'),
('p15','berry-marmalade','KOL-355','Ягодный мармелад ассорти','Мармелад · ягоды','Ассорти ягодных вкусов.',57900,0,'коробка 200 г',200,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-marmalade','m-kolomna',ARRAY['ягоды','без глютена'],ARRAY[]::text[],'','',150,4.6,91,19,false,'2026-06-22'),
('p16','butter-cookies','MIC-701','Сливочное печенье Sablés','Sablés · сливочное','Рассыпчатое французское печенье.',68900,74900,'коробка 150 г',150,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-cookies','m-michel',ARRAY['сливочное'],ARRAY['−8%'],'','',180,4.8,151,26,true,'2026-06-30'),
('p17','chocolate-cookies','MIC-730','Печенье с шоколадной крошкой','Печенье · шоколад','Хрустящее печенье с тёмным шоколадом.',72900,0,'коробка 180 г',180,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-cookies','m-michel',ARRAY['шоколад'],ARRAY['Хит'],'','',180,4.9,226,11,true,'2026-07-01'),
('p18','wafer-hazelnut','CAF-811','Вафли с кремом из фундука','Вафли · фундук','Тонкие вафли с ореховым кремом.',59900,0,'упаковка 125 г',125,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-cookies','m-caffarel',ARRAY['орехи'],ARRAY['Нет в наличии'],'','',180,4.5,66,0,false,'2026-07-07'),
('p19','dark-orange-gift','LEO-905','Набор тёмного шоколада с апельсином','Шоколад · апельсин','Подарочная коллекция тёмного шоколада.',289000,319000,'набор 320 г',320,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-gifts','m-leonidas',ARRAY['цитрус','подарок'],ARRAY['−9%'],'','',180,4.9,110,8,true,'2026-07-10'),
('p20','atelier-signature-box','CHA-999','Signature Box: 24 конфеты','Signature Box','Авторское ассорти из 24 конфет.',329000,0,'набор 24 шт.',420,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-gifts','m-choco',ARRAY['подарок'],ARRAY['Бестселлер'],'','',90,5.0,193,5,true,'2026-07-15'),
('p21','pastila-tea-box','BEL-920','Чайная коллекция пастилы','Пастила · ассорти','Подарочный набор фруктовой пастилы.',149000,0,'набор 480 г',480,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-gifts','m-belyov',ARRAY['без сахара','подарок'],ARRAY[]::text[],'','',120,4.8,77,12,false,'2026-07-20'),
('p22','white-matcha-chocolate','SWE-260','Белый шоколад с матчей','Белый · матча','Белый шоколад с японским чаем матча.',74900,0,'плитка 90 г',90,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-bars','m-sweetzerland',ARRAY['матча'],ARRAY['Новинка'],'','',180,4.4,43,16,false,'2026-08-02'),
('p23','caramel-praline','LEO-522','Пралине с хрустящей карамелью','Пралине · карамель','Бельгийское пралине с карамелью.',179000,0,'коробка 190 г',190,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-praline','m-leonidas',ARRAY['карамель','подарок'],ARRAY[]::text[],'','',120,4.8,104,14,false,'2026-07-25'),
('p24','lemon-sable','MIC-745','Лимонное печенье Sablés','Sablés · лимон','Сливочное печенье с лимонной цедрой.',69900,0,'коробка 150 г',150,'/assets/product-chocolate.webp',ARRAY[]::text[],'cat-cookies','m-michel',ARRAY['цитрус'],ARRAY[]::text[],'','',180,4.6,68,23,false,'2026-07-31');

UPDATE products SET
    image = CASE
        WHEN category_id IN ('cat-marshmallow','cat-marmalade') THEN '/assets/product-marshmallow.webp'
        WHEN category_id IN ('cat-cookies','cat-gifts') THEN '/assets/product-giftbox.webp'
        ELSE '/assets/product-chocolate.webp'
    END,
    gallery = ARRAY[CASE
        WHEN category_id IN ('cat-marshmallow','cat-marmalade') THEN '/assets/product-marshmallow.webp'
        WHEN category_id IN ('cat-cookies','cat-gifts') THEN '/assets/product-giftbox.webp'
        ELSE '/assets/product-chocolate.webp'
    END]
WHERE id IN ('p01','p02','p03','p04','p05','p06','p07','p08','p09','p10','p11','p12','p13','p14','p15','p16','p17','p18','p19','p20','p21','p22','p23','p24');

UPDATE products SET series = CASE
    WHEN id IN ('p01','p08') THEN 'Atelier Origin'
    WHEN id IN ('p06','p20') THEN 'Atelier Ganache'
    WHEN id IN ('p02','p04','p18') THEN 'Piemonte Classico'
    WHEN id IN ('p05','p19','p23') THEN 'Belgian Selection'
    WHEN id IN ('p10','p11','p12','p21') THEN 'Белёвская классика'
    WHEN id IN ('p16','p17','p24') THEN 'Les Sablés'
    ELSE '' END
WHERE id IN ('p01','p02','p03','p04','p05','p06','p07','p08','p09','p10','p11','p12','p13','p14','p15','p16','p17','p18','p19','p20','p21','p22','p23','p24');

UPDATE products SET brand_id = CASE
    WHEN id IN ('p01','p08') THEN 'b-atelier-noir'
    WHEN id IN ('p06','p20') THEN 'b-atelier-maison'
    WHEN id = 'p02' THEN 'b-caffarel'
    WHEN id IN ('p04','p18') THEN 'b-piemonte'
    WHEN id IN ('p05','p19') THEN 'b-leonidas'
    WHEN id = 'p23' THEN 'b-manon'
    WHEN id IN ('p03','p07','p22') THEN 'b-sweetzerland'
    WHEN id IN ('p10','p11','p12','p21') THEN 'b-belev'
    WHEN id IN ('p13','p14','p15') THEN 'b-kolomna'
    WHEN id IN ('p16','p17','p24') THEN 'b-michel'
    WHEN id = 'p09' THEN 'b-lakrids'
    ELSE 'brand-unbranded' END
WHERE id IN ('p01','p02','p03','p04','p05','p06','p07','p08','p09','p10','p11','p12','p13','p14','p15','p16','p17','p18','p19','p20','p21','p22','p23','p24');

INSERT INTO collections (id, slug, name, description, accent, sort_order) VALUES
('col-new','new','Новинки недели','Свежие вкусы от любимых мануфактур','lime',10),
('col-bestsellers','bestsellers','Берут чаще всего','Проверенные фавориты покупателей','cocoa',20),
('col-gifts','gifts-under-2000','Подарки до 2 000 ₽','Готовые наборы без долгого выбора','coral',30),
('col-no-sugar','no-added-sugar','Без добавленного сахара','Чистый вкус фруктов и какао','sky',40);

INSERT INTO collection_products (collection_id, product_id, sort_order) VALUES
('col-new','p02',1),('col-new','p03',2),('col-new','p13',3),('col-new','p22',4),
('col-bestsellers','p01',1),('col-bestsellers','p05',2),('col-bestsellers','p10',3),('col-bestsellers','p17',4),('col-bestsellers','p20',5),
('col-gifts','p04',1),('col-gifts','p06',2),('col-gifts','p07',3),('col-gifts','p21',4),('col-gifts','p23',5),
('col-no-sugar','p01',1),('col-no-sugar','p10',2),('col-no-sugar','p11',3),('col-no-sugar','p21',4);

INSERT INTO cart_items (user_id, product_id, quantity, selected) VALUES
('usr-demo','p01',2,true),('usr-demo','p10',3,true),('usr-demo','p16',1,false),('usr-demo','p18',2,false);
INSERT INTO favorites (user_id, product_id) VALUES
('usr-demo','p02'),('usr-demo','p05'),('usr-demo','p09'),('usr-demo','p14'),('usr-demo','p20');

INSERT INTO orders (id, number, user_id, status, status_label, item_count, total_minor, delivery_address, created_at, delivered_at) VALUES
('ord-2026-002','#10482','usr-demo','delivered','Доставлен',19,502600,'Москва, ул. Большая Дмитровка, 12','2026-07-18T12:30:00Z','2026-07-20T16:15:00Z');
INSERT INTO order_items (order_id, position, product_id, sku, name, image, manufacturer_id, manufacturer, quantity, unit_price_minor, line_total_minor, replacement_product_id) VALUES
('ord-2026-002',1,'p01','CHA-701','Тёмный шоколад 70% с морской солью','/assets/product-chocolate.webp','m-choco','Choco Atelier',2,45900,91800,''),
('ord-2026-002',2,'p07','SWE-418','Трюфели Espresso','/assets/product-chocolate.webp','m-sweetzerland','Sweetzerland',12,139000,1668000,''),
('ord-2026-002',3,'p18','CAF-811','Вафли с кремом из фундука','/assets/product-chocolate.webp','m-caffarel','Caffarel',3,59900,179700,''),
('ord-2026-002',4,'legacy-01','OLD-101','Карамель Coffee Archive','','m-choco','Choco Atelier',2,39000,78000,'p06');
UPDATE order_items oi SET image = p.image FROM products p WHERE oi.order_id = 'ord-2026-002' AND oi.product_id = p.id;
UPDATE orders SET total_minor = (SELECT sum(line_total_minor) FROM order_items WHERE order_id='ord-2026-002') WHERE id='ord-2026-002';
