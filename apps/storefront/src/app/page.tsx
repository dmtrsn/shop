import { Container } from "@/components/ui/container";

import styles from "./home.module.css";

type PlaceholderSectionProps = {
  count: number;
  description: string;
  heading: string;
  productShape?: boolean;
  surface?: boolean;
};

function PlaceholderSection({
  count,
  description,
  heading,
  productShape = false,
  surface = false,
}: PlaceholderSectionProps) {
  return (
    <section className={`${styles.section} ${surface ? styles.sectionAlt : ""}`}>
      <Container>
        <div className={styles.sectionHeader}>
          <h2>{heading}</h2>
          <p>{description}</p>
        </div>
        <div
          aria-label={`${heading}: структурный заполнитель`}
          className={`${styles.placeholderGrid} ${productShape ? styles.placeholderGridProducts : ""}`}
        >
          {Array.from({ length: count }, (_, index) => (
            <div className={styles.placeholder} key={index}>
              {productShape ? <div aria-hidden="true" className={styles.placeholderMedia} /> : null}
              <div aria-hidden="true" className={styles.placeholderLine} />
              <div aria-hidden="true" className={styles.placeholderLineShort} />
              <p className={styles.placeholderText}>Данные появятся после подключения API</p>
            </div>
          ))}
        </div>
      </Container>
    </section>
  );
}

export default function Home() {
  return (
    <>
      <main className={styles.main} id="main-content">
        <Container className={styles.hero}>
          <div>
            <p className={styles.eyebrow}>Предпросмотр новой витрины</p>
            <h1 className={styles.title}>Спокойная основа для удобных покупок</h1>
            <p className={styles.lede}>
              Это изолированный интерфейсный фундамент. Каталог, цены, корзина и аккаунт пока
              остаются в действующей версии магазина.
            </p>
          </div>
          <div className={styles.heroPanel}>
            <p>
              Здесь появится утверждённая редакционная композиция с реальной фотографией. Сейчас
              блок намеренно не имитирует товар или предложение.
            </p>
          </div>
        </Container>

        <PlaceholderSection
          count={6}
          description="Будущая навигация по реальной таксономии без вымышленных категорий."
          heading="Категории"
        />
        <PlaceholderSection
          count={2}
          description="Место для опубликованных редакционных подборок после подключения контракта."
          heading="Подборки"
          productShape
          surface
        />
        <PlaceholderSection
          count={4}
          description="Здесь будут товары только с документированным основанием отбора."
          heading="Выбранные товары"
          productShape
        />
        <PlaceholderSection
          count={3}
          description="Структура будущего каталога производителей без фиктивных названий и стран."
          heading="Производители"
          surface
        />
      </main>
      <footer className={styles.footer}>
        <Container>Основа будущей витрины · без подключения коммерческих данных</Container>
      </footer>
    </>
  );
}
